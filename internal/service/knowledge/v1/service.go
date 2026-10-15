// Package knowledge runs the knowledge-base use cases: upload a file,
// extract its pages in the background, browse them, and hand an agent
// exactly the referenced pages.
package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/blob"
	"kurgu/internal/db"
	jobdomain "kurgu/internal/domain/job/v1"
	knowledgedomain "kurgu/internal/domain/knowledge/v1"
	knowledgerepo "kurgu/internal/repository/knowledge/v1"
	"kurgu/pkg/id"
)

// KindExtract is the job kind that extracts a document's pages.
const KindExtract = "knowledge.extract"

// ExtractPayload is the knowledge.extract job payload.
type ExtractPayload struct {
	DocumentID uuid.UUID `json:"documentId"`
}

// Extractor turns a file into pages and its table of contents
// (internal/adapter/extract).
type Extractor interface {
	Extract(ctx context.Context, format knowledgedomain.Format, path string) (knowledgedomain.Extraction, error)
}

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// UsageChecker reports which pipelines reference a document, so a
// document in use is not deleted from under them.
type UsageChecker interface {
	PipelinesUsing(ctx context.Context, workspaceID, documentID uuid.UUID) ([]string, error)
}

// Service runs knowledge use cases.
type Service struct {
	repo      knowledgerepo.Repository
	blobs     blob.Store
	jobs      Enqueuer
	extractor Extractor
	usage     UsageChecker
	tx        db.TxManager
	now       func() time.Time
}

// NewService returns a Service. usage may be nil until pipelines exist.
func NewService(repo knowledgerepo.Repository, blobs blob.Store, jobs Enqueuer, extractor Extractor, tx db.TxManager) *Service {
	return &Service{repo: repo, blobs: blobs, jobs: jobs, extractor: extractor, tx: tx, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// WithUsage sets the usage checker (wired after the pipeline service).
func (s *Service) WithUsage(u UsageChecker) *Service {
	s.usage = u
	return s
}

// Upload stores the file and queues its extraction.
func (s *Service) Upload(ctx context.Context, workspaceID, actorID uuid.UUID, title, filename string, r io.Reader) (*knowledgedomain.Document, error) {
	if _, err := knowledgedomain.FormatFromFilename(filename); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "kurgu-upload-*")
	if err != nil {
		return nil, fmt.Errorf("temp file: %w", err)
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, knowledgedomain.MaxFileBytes+1))
	if err != nil {
		return nil, apperror.Wrap(apperror.BadRequest, err, "upload interrupted")
	}
	d, err := knowledgedomain.New(id.New(), workspaceID, actorID, title,
		knowledgedomain.Upload{Filename: filename, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, s.now())
	if err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := s.blobs.Put(ctx, d.BlobKey(), tmp, n); err != nil {
		return nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, d); err != nil {
			return err
		}
		return s.enqueueExtract(ctx, d)
	})
	if err != nil {
		_ = s.blobs.Delete(context.WithoutCancel(ctx), d.BlobKey())
		return nil, err
	}
	return d, nil
}

func (s *Service) enqueueExtract(ctx context.Context, d *knowledgedomain.Document) error {
	payload, _ := json.Marshal(ExtractPayload{DocumentID: d.ID()})
	_, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{
		WorkspaceID: d.WorkspaceID(),
		Queue:       jobdomain.QueueExtract,
		Kind:        KindExtract,
		Payload:     payload,
		MaxAttempts: 2,
	})
	return err
}

// Extract runs a knowledge.extract job. A file that cannot be read as
// its format marks the document failed (no retry); storage errors are
// returned so the job retries.
func (s *Service) Extract(ctx context.Context, documentID uuid.UUID) error {
	d, err := s.repo.GetByID(ctx, documentID)
	if apperror.Is(err, apperror.DocumentNotFound) {
		return jobdomain.Permanent(err) // deleted before extraction ran
	}
	if err != nil {
		return err
	}
	if d.Status() != knowledgedomain.StatusPending {
		return nil
	}
	path, cleanup, err := s.download(ctx, d)
	if err != nil {
		return err
	}
	defer cleanup()
	ex, extractErr := s.extractor.Extract(ctx, d.Format(), path)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if extractErr != nil {
			_ = d.Failed(extractErr.Error(), s.now())
			return s.repo.Update(ctx, d)
		}
		if err := d.Extracted(ex.Pages, s.now()); err != nil {
			return err
		}
		if d.Status() == knowledgedomain.StatusReady {
			if err := s.repo.ReplacePages(ctx, d.ID(), ex.Pages); err != nil {
				return err
			}
			if err := s.repo.ReplaceHeadings(ctx, d.ID(), knowledgedomain.NormalizeHeadings(ex.Headings, d.PageCount())); err != nil {
				return err
			}
		}
		return s.repo.Update(ctx, d)
	})
}

func (s *Service) download(ctx context.Context, d *knowledgedomain.Document) (string, func(), error) {
	rc, _, err := s.blobs.Open(ctx, d.BlobKey())
	if err != nil {
		return "", nil, fmt.Errorf("open original: %w", err)
	}
	defer func() { _ = rc.Close() }()
	tmp, err := os.CreateTemp("", "kurgu-extract-*."+string(d.Format()))
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	if _, err := io.Copy(tmp, rc); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", nil, fmt.Errorf("download original: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmp.Name(), cleanup, nil
}

// Retry queues a failed or ready document for extraction again.
func (s *Service) Retry(ctx context.Context, workspaceID, documentID uuid.UUID) (*knowledgedomain.Document, error) {
	var out *knowledgedomain.Document
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		d, err := s.repo.Get(ctx, workspaceID, documentID)
		if err != nil {
			return err
		}
		if err := d.Retry(s.now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, d); err != nil {
			return err
		}
		out = d
		return s.enqueueExtract(ctx, d)
	})
	return out, err
}

// Get returns one document.
func (s *Service) Get(ctx context.Context, workspaceID, documentID uuid.UUID) (*knowledgedomain.Document, error) {
	return s.repo.Get(ctx, workspaceID, documentID)
}

// List returns the workspace's documents, newest first.
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID) ([]*knowledgedomain.Document, error) {
	return s.repo.List(ctx, workspaceID)
}

// Rename changes a document's title.
func (s *Service) Rename(ctx context.Context, workspaceID, documentID uuid.UUID, title string) (*knowledgedomain.Document, error) {
	var out *knowledgedomain.Document
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		d, err := s.repo.Get(ctx, workspaceID, documentID)
		if err != nil {
			return err
		}
		if err := d.Rename(title, s.now()); err != nil {
			return err
		}
		out = d
		return s.repo.Update(ctx, d)
	})
	return out, err
}

// Delete removes a document and its file, unless a pipeline uses it.
func (s *Service) Delete(ctx context.Context, workspaceID, documentID uuid.UUID) error {
	d, err := s.repo.Get(ctx, workspaceID, documentID)
	if err != nil {
		return err
	}
	if s.usage != nil {
		names, err := s.usage.PipelinesUsing(ctx, workspaceID, documentID)
		if err != nil {
			return err
		}
		if len(names) > 0 {
			return apperror.New(apperror.Conflict, "used by pipeline "+strings.Join(names, ", ")+"; remove the reference first")
		}
	}
	if err := s.repo.Delete(ctx, workspaceID, documentID); err != nil {
		return err
	}
	return s.blobs.DeletePrefix(ctx, strings.TrimSuffix(d.BlobKey(), "/original."+string(d.Format())))
}

// Pages returns pages from..to (to 0 = to the end).
func (s *Service) Pages(ctx context.Context, workspaceID, documentID uuid.UUID, from, to int) (*knowledgedomain.Document, []knowledgedomain.Page, error) {
	d, err := s.repo.Get(ctx, workspaceID, documentID)
	if err != nil {
		return nil, nil, err
	}
	pages, err := s.repo.Pages(ctx, documentID, from, to)
	return d, pages, err
}

// Contents returns the document's table of contents as sections with
// page ranges; empty when the file declares none.
func (s *Service) Contents(ctx context.Context, workspaceID, documentID uuid.UUID) ([]knowledgedomain.Section, error) {
	d, err := s.repo.Get(ctx, workspaceID, documentID)
	if err != nil {
		return nil, err
	}
	hs, err := s.repo.Headings(ctx, documentID)
	if err != nil {
		return nil, err
	}
	return knowledgedomain.Contents(hs, d.PageCount()), nil
}

// Open returns the original file.
func (s *Service) Open(ctx context.Context, workspaceID, documentID uuid.UUID) (*knowledgedomain.Document, io.ReadCloser, int64, error) {
	d, err := s.repo.Get(ctx, workspaceID, documentID)
	if err != nil {
		return nil, nil, 0, err
	}
	rc, info, err := s.blobs.Open(ctx, d.BlobKey())
	if errors.Is(err, blob.ErrNotFound) {
		return nil, nil, 0, apperror.New(apperror.DocumentNotFound, "the original file is missing")
	}
	if err != nil {
		return nil, nil, 0, err
	}
	return d, rc, info.Size, nil
}

// CheckRange verifies that a pipeline may reference pages from..to of a
// document (0, 0 = whole document).
func (s *Service) CheckRange(ctx context.Context, workspaceID, documentID uuid.UUID, from, to int) error {
	d, err := s.repo.Get(ctx, workspaceID, documentID)
	if apperror.Is(err, apperror.DocumentNotFound) {
		return apperror.New(apperror.ValidationFailed, "referenced document does not exist")
	}
	if err != nil {
		return err
	}
	return d.CheckRange(from, to)
}

// Ref selects pages of a document for an agent.
type Ref struct {
	DocumentID uuid.UUID
	From, To   int // 0, 0 = whole document
}

// Excerpt is the referenced text, ready to give an agent.
type Excerpt struct {
	Markdown string
	Tokens   int
	Pages    int
}

// Excerpt concatenates the referenced pages, each headed with its
// document and page number so the agent can cite them.
func (s *Service) Excerpt(ctx context.Context, workspaceID uuid.UUID, refs []Ref) (Excerpt, error) {
	var (
		b   strings.Builder
		out Excerpt
	)
	for _, ref := range refs {
		d, pages, err := s.Pages(ctx, workspaceID, ref.DocumentID, ref.From, ref.To)
		if err != nil {
			return Excerpt{}, err
		}
		fmt.Fprintf(&b, "# %s\n\n", d.Title())
		for _, p := range pages {
			if p.Text == "" {
				continue
			}
			b.WriteString("## " + d.Title() + ", page " + strconv.Itoa(p.Number) + "\n\n")
			b.WriteString(p.Text)
			b.WriteString("\n\n")
			out.Tokens += p.Tokens
			out.Pages++
		}
	}
	out.Markdown = b.String()
	return out, nil
}

// PageCost is a page's token estimate.
type PageCost struct {
	Number, Tokens int
}

// PageTokens returns the token estimate of each page from..to (0, 0 =
// whole document), for budgeting what an agent is given.
func (s *Service) PageTokens(ctx context.Context, workspaceID, documentID uuid.UUID, from, to int) ([]PageCost, error) {
	_, pages, err := s.Pages(ctx, workspaceID, documentID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]PageCost, 0, len(pages))
	for _, p := range pages {
		out = append(out, PageCost{Number: p.Number, Tokens: p.Tokens})
	}
	return out, nil
}
