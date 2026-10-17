// Package library runs the media library use cases: upload music,
// videos and photos, ingest them in the background, browse, describe,
// archive and delete them.
package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/blob"
	"kurgu/internal/db"
	jobdomain "kurgu/internal/domain/job/v1"
	librarydomain "kurgu/internal/domain/library/v1"
	libraryrepo "kurgu/internal/repository/library/v1"
	"kurgu/pkg/id"
)

// KindIngest is the job kind that processes an uploaded file.
const KindIngest = "library.ingest"

// IngestPayload is the library.ingest job payload.
type IngestPayload struct {
	AssetID uuid.UUID `json:"assetId"`
}

// Processor turns an uploaded file into the stored main file (and a
// poster for pictures) in outDir (internal/adapter/media).
type Processor interface {
	Ingest(ctx context.Context, kind librarydomain.Kind, in, outDir string) (librarydomain.Media, error)
}

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// Filter narrows a listing.
type Filter = libraryrepo.Filter

// Patch is a partial update: nil fields stay as they are.
type Patch struct {
	Details  *librarydomain.Details
	Archived *bool
}

// Service runs library use cases.
type Service struct {
	repo      libraryrepo.Repository
	blobs     blob.Store
	jobs      Enqueuer
	processor Processor
	tx        db.TxManager
	now       func() time.Time
}

// NewService returns a Service.
func NewService(repo libraryrepo.Repository, blobs blob.Store, jobs Enqueuer, processor Processor, tx db.TxManager) *Service {
	return &Service{repo: repo, blobs: blobs, jobs: jobs, processor: processor, tx: tx, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Upload stores the file and queues its ingestion. An empty kind is
// taken from the file name. A file the workspace already holds is
// refused with Conflict.
func (s *Service) Upload(ctx context.Context, workspaceID, actorID uuid.UUID, kind librarydomain.Kind, d librarydomain.Details,
	filename string, r io.Reader) (*librarydomain.Asset, error) {
	fileKind, err := librarydomain.KindFromFilename(filename)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		kind = fileKind
	}
	tmp, err := os.CreateTemp("", "kurgu-upload-*")
	if err != nil {
		return nil, fmt.Errorf("temp file: %w", err)
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, kind.MaxBytes()+1))
	if err != nil {
		return nil, apperror.Wrap(apperror.BadRequest, err, "upload interrupted")
	}
	sum := hex.EncodeToString(h.Sum(nil))
	a, err := librarydomain.NewUpload(id.New(), workspaceID, actorID, kind,
		librarydomain.Upload{Filename: filename, Size: n, SHA256: sum}, d, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.checkNew(ctx, workspaceID, sum); err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := s.blobs.Put(ctx, a.OriginalKey(), tmp, n); err != nil {
		return nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, a); err != nil {
			return err
		}
		return s.enqueueIngest(ctx, a)
	})
	if err != nil {
		_ = s.blobs.DeletePrefix(context.WithoutCancel(ctx), a.Prefix())
		return nil, err
	}
	return a, nil
}

// checkNew refuses a file the workspace already holds, naming it.
func (s *Service) checkNew(ctx context.Context, workspaceID uuid.UUID, sum string) error {
	existing, err := s.repo.FindBySHA256(ctx, workspaceID, sum)
	switch {
	case err == nil:
		return apperror.New(apperror.Conflict, "the library already holds this file as “"+existing.Title()+"”")
	case apperror.Is(err, apperror.AssetNotFound):
		return nil
	}
	return err
}

func (s *Service) enqueueIngest(ctx context.Context, a *librarydomain.Asset) error {
	payload, _ := json.Marshal(IngestPayload{AssetID: a.ID()})
	_, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{
		WorkspaceID: a.WorkspaceID(),
		Queue:       jobdomain.QueueExtract,
		Kind:        KindIngest,
		Payload:     payload,
		MaxAttempts: 2,
	})
	return err
}

// Ingest runs a library.ingest job. A file that cannot be processed
// marks the asset failed and keeps the original for a retry; storage
// errors are returned so the job retries.
func (s *Service) Ingest(ctx context.Context, assetID uuid.UUID) error {
	a, err := s.repo.GetByID(ctx, assetID)
	if apperror.Is(err, apperror.AssetNotFound) {
		return jobdomain.Permanent(err) // deleted before ingestion ran
	}
	if err != nil {
		return err
	}
	if a.Status() != librarydomain.StatusProcessing {
		return nil
	}
	dir, err := os.MkdirTemp("", "kurgu-ingest-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	in := filepath.Join(dir, "original"+filepath.Ext(a.OriginalKey()))
	if err := s.download(ctx, a.OriginalKey(), in); err != nil {
		return err
	}
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o750); err != nil {
		return err
	}
	m, ingestErr := s.processor.Ingest(ctx, a.Kind(), in, out)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if ingestErr == nil {
		// Ingested only reports what the asset will become; the files go
		// up before the row says they exist.
		probe, _ := librarydomain.Rehydrate(a.Snapshot())
		if err := probe.Ingested(m, s.now()); err != nil {
			return err
		}
		if err := s.putFiles(ctx, probe, out); err != nil {
			return err
		}
	}
	var ready bool
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// Processing took a while: apply its outcome to the asset as it
		// is now, so an edit meanwhile is kept and a deletion or another
		// run's outcome is not overwritten.
		a, err := s.repo.GetByID(ctx, assetID)
		if apperror.Is(err, apperror.AssetNotFound) {
			return jobdomain.Permanent(err)
		}
		if err != nil {
			return err
		}
		if a.Status() != librarydomain.StatusProcessing {
			return nil
		}
		if ingestErr != nil {
			_ = a.Failed(ingestErr.Error(), s.now())
		} else if err := a.Ingested(m, s.now()); err != nil {
			return err
		}
		ready = a.Status() == librarydomain.StatusReady
		return s.repo.Update(ctx, a)
	})
	if err != nil {
		return err
	}
	if ready {
		// The stored files replace the original; a retry is never needed.
		_ = s.blobs.Delete(ctx, a.OriginalKey())
	}
	return nil
}

// putFiles uploads the ingested files of a (ready) asset from dir.
func (s *Service) putFiles(ctx context.Context, a *librarydomain.Asset, dir string) error {
	if a.Status() != librarydomain.StatusReady {
		return nil
	}
	for _, f := range []librarydomain.File{librarydomain.FileMain, librarydomain.FilePoster} {
		key, ok := a.Key(f)
		if !ok {
			continue
		}
		if err := s.putFile(ctx, key, filepath.Join(dir, filepath.Base(key))); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) putFile(ctx context.Context, key, path string) error {
	f, err := os.Open(path) //nolint:gosec // our own temp files
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return s.blobs.Put(ctx, key, f, st.Size())
}

func (s *Service) download(ctx context.Context, key, path string) error {
	rc, _, err := s.blobs.Open(ctx, key)
	if errors.Is(err, blob.ErrNotFound) {
		return jobdomain.Permanent(fmt.Errorf("the uploaded file is missing: %w", err))
	}
	if err != nil {
		return fmt.Errorf("open original: %w", err)
	}
	defer func() { _ = rc.Close() }()
	f, err := os.Create(path) //nolint:gosec // our own temp dir
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, rc); err != nil {
		_ = f.Close()
		return fmt.Errorf("download original: %w", err)
	}
	return f.Close()
}

// Retry queues a failed asset for ingestion again.
func (s *Service) Retry(ctx context.Context, workspaceID, assetID uuid.UUID) (*librarydomain.Asset, error) {
	var out *librarydomain.Asset
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		a, err := s.repo.Get(ctx, workspaceID, assetID)
		if err != nil {
			return err
		}
		if err := a.Retry(s.now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, a); err != nil {
			return err
		}
		out = a
		return s.enqueueIngest(ctx, a)
	})
	return out, err
}

// Get returns one asset.
func (s *Service) Get(ctx context.Context, workspaceID, assetID uuid.UUID) (*librarydomain.Asset, error) {
	return s.repo.Get(ctx, workspaceID, assetID)
}

// List returns a page of the workspace's assets and the next cursor.
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*librarydomain.Asset, string, error) {
	return s.repo.List(ctx, workspaceID, f)
}

// Update describes, archives or unarchives an asset.
func (s *Service) Update(ctx context.Context, workspaceID, assetID uuid.UUID, p Patch) (*librarydomain.Asset, error) {
	var out *librarydomain.Asset
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		a, err := s.repo.Get(ctx, workspaceID, assetID)
		if err != nil {
			return err
		}
		if p.Details != nil {
			if err := a.Describe(*p.Details, s.now()); err != nil {
				return err
			}
		}
		if p.Archived != nil {
			if *p.Archived {
				a.Archive(s.now())
			} else {
				a.Unarchive(s.now())
			}
		}
		out = a
		return s.repo.Update(ctx, a)
	})
	return out, err
}

// Delete removes an asset and its files. Revisions that used it keep
// their own copies.
func (s *Service) Delete(ctx context.Context, workspaceID, assetID uuid.UUID) error {
	a, err := s.repo.Get(ctx, workspaceID, assetID)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, workspaceID, assetID); err != nil {
		return err
	}
	return s.blobs.DeletePrefix(ctx, a.Prefix())
}

// Open returns one of an asset's files.
func (s *Service) Open(ctx context.Context, workspaceID, assetID uuid.UUID, f librarydomain.File) (*librarydomain.Asset, io.ReadCloser, int64, error) {
	a, err := s.repo.Get(ctx, workspaceID, assetID)
	if err != nil {
		return nil, nil, 0, err
	}
	key, ok := a.Key(f)
	if !ok {
		return nil, nil, 0, apperror.New(apperror.AssetNotFound, "the asset has no such file yet")
	}
	rc, info, err := s.blobs.Open(ctx, key)
	if errors.Is(err, blob.ErrNotFound) {
		return nil, nil, 0, apperror.New(apperror.AssetNotFound, "the file is missing")
	}
	if err != nil {
		return nil, nil, 0, err
	}
	return a, rc, info.Size, nil
}
