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
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/blob"
	"github.com/ahmethakanbesel/ekin/internal/db"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	librarydomain "github.com/ahmethakanbesel/ekin/internal/domain/library/v1"
	libraryrepo "github.com/ahmethakanbesel/ekin/internal/repository/library/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// KindIngest is the job kind that processes an uploaded file.
const KindIngest = "library.ingest"

// IngestPayload is the library.ingest job payload.
type IngestPayload struct {
	AssetID uuid.UUID `json:"assetId"`
}

// Processor turns an uploaded file into the stored main file (and a
// poster for pictures) in outDir (internal/adapter/media). Inspect does
// the same for a file already in its stored format, writing only the
// poster; Preview encodes the start of a rendered score.
type Processor interface {
	Ingest(ctx context.Context, kind librarydomain.Kind, in, outDir string) (librarydomain.Media, error)
	Inspect(ctx context.Context, kind librarydomain.Kind, in, outDir string) (librarydomain.Media, error)
	Preview(ctx context.Context, in, out string) error
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
	tmp, err := os.CreateTemp("", "ekin-upload-*")
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
	dir, err := os.MkdirTemp("", "ekin-ingest-*")
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

// UsageCounts returns how many deliverables used each asset; unused ones
// are absent.
func (s *Service) UsageCounts(ctx context.Context, workspaceID uuid.UUID, assetIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	return s.repo.UsageCounts(ctx, workspaceID, assetIDs)
}

// Usages returns where an asset was used, newest first.
func (s *Service) Usages(ctx context.Context, workspaceID, assetID uuid.UUID) ([]librarydomain.Usage, error) {
	if _, err := s.repo.Get(ctx, workspaceID, assetID); err != nil {
		return nil, err
	}
	return s.repo.Usages(ctx, workspaceID, assetID)
}

// RecordUsage notes that a revision used the assets; ids not in the
// workspace's library are ignored (the agent may have invented one).
func (s *Service) RecordUsage(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, revision int, assetIDs []uuid.UUID) error {
	for _, assetID := range assetIDs {
		u, err := librarydomain.NewUsage(assetID, workspaceID, storyID, deliverableID, revision, s.now())
		if err != nil {
			return err
		}
		if err := s.repo.RecordUsage(ctx, u); err != nil {
			return err
		}
	}
	return nil
}

// catalogLimit bounds how many assets of one kind a draft is offered.
const catalogLimit = 500

// Entry is an asset offered to a draft, with how often it was used.
type Entry struct {
	Asset *librarydomain.Asset
	Uses  int
}

// Catalog lists the usable assets of kinds that fit a frame of
// orientation, least used first and then newest: what a draft may take
// from the library.
func (s *Service) Catalog(ctx context.Context, workspaceID uuid.UUID, kinds []librarydomain.Kind, orientation librarydomain.Orientation) ([]Entry, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	assets, _, err := s.repo.List(ctx, workspaceID, Filter{Kinds: kinds, Statuses: []librarydomain.Status{librarydomain.StatusReady},
		Limit: catalogLimit * len(kinds)})
	if err != nil {
		return nil, err
	}
	assets = slices.DeleteFunc(assets, func(a *librarydomain.Asset) bool { return !a.Usable() || !a.Fits(orientation) })
	ids := make([]uuid.UUID, len(assets))
	for i, a := range assets {
		ids[i] = a.ID()
	}
	counts, err := s.repo.UsageCounts(ctx, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(assets))
	for i, a := range assets {
		out[i] = Entry{Asset: a, Uses: counts[a.ID()]}
	}
	// Stable: the listing is newest first, so equal counts stay that way.
	slices.SortStableFunc(out, func(a, b Entry) int { return a.Uses - b.Uses })
	return out, nil
}

// Collect returns which kinds the workspace saves from its drafts.
func (s *Service) Collect(ctx context.Context, workspaceID uuid.UUID) (librarydomain.Collect, error) {
	return s.repo.GetCollect(ctx, workspaceID)
}

// SaveCollect sets which kinds the workspace saves from its drafts.
func (s *Service) SaveCollect(ctx context.Context, workspaceID uuid.UUID, c librarydomain.Collect) (librarydomain.Collect, error) {
	return c, s.repo.SaveCollect(ctx, workspaceID, c, s.now())
}

// Harvestable is a file a draft used that may join the library: Pexels
// stock in its stored format (webm, jpg) or a composed score (music.js)
// with its rendered WAV for the preview.
type Harvestable struct {
	Kind    librarydomain.Kind
	Origin  librarydomain.Origin
	Harvest librarydomain.Harvest
	Details librarydomain.Details
	Path    string // the main file
	Preview string // a score's rendered WAV; "" otherwise
}

// Find returns the library asset a harvestable file already is, if any:
// the same outside item, or the same file.
func (s *Service) Find(ctx context.Context, workspaceID uuid.UUID, h Harvestable) (*librarydomain.Asset, error) {
	var (
		a   *librarydomain.Asset
		err error
	)
	if h.Harvest.SourceRef != "" {
		a, err = s.repo.FindBySourceRef(ctx, workspaceID, h.Harvest.SourceRef)
	} else {
		var sum string
		if sum, _, err = hashFile(h.Path); err != nil {
			return nil, err
		}
		a, err = s.repo.FindBySHA256(ctx, workspaceID, sum)
	}
	if apperror.Is(err, apperror.AssetNotFound) {
		return nil, nil
	}
	return a, err
}

// Save keeps a draft's file in the library, once: a file it already holds
// is returned as it is (and, saved by a person, kept). An automatic
// harvest saves it provisional, a person's save kept. The revision it
// came from counts as a use.
func (s *Service) Save(ctx context.Context, workspaceID, actorID uuid.UUID, h Harvestable, kept bool) (*librarydomain.Asset, error) {
	existing, err := s.Find(ctx, workspaceID, h)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if kept && (!existing.Kept() || existing.Provisional()) {
			existing.Keep(s.now())
			if err := s.repo.Update(ctx, existing); err != nil {
				return nil, err
			}
		}
		return existing, nil
	}
	dir, err := os.MkdirTemp("", "ekin-harvest-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	var m librarydomain.Media
	if h.Kind == librarydomain.KindMusic {
		m = librarydomain.Media{Format: "js"}
		if h.Preview != "" {
			if err := s.processor.Preview(ctx, h.Preview, filepath.Join(dir, "preview.opus")); err != nil {
				return nil, err
			}
		}
	} else if m, err = s.processor.Inspect(ctx, h.Kind, h.Path, dir); err != nil {
		return nil, err
	}
	sum, size, err := hashFile(h.Path)
	if err != nil {
		return nil, err
	}
	a, err := librarydomain.NewHarvested(id.New(), workspaceID, actorID, h.Kind, h.Origin, h.Harvest,
		librarydomain.Saved{SHA256: sum, Size: size, Media: m}, h.Details, kept, s.now())
	if err != nil {
		return nil, err
	}
	main, _ := a.Key(librarydomain.FileMain)
	if err := s.putFile(ctx, main, h.Path); err != nil {
		return nil, err
	}
	for _, f := range []librarydomain.File{librarydomain.FilePoster, librarydomain.FilePreview} {
		key, ok := a.Key(f)
		if !ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.Base(key))); err != nil {
			continue // a score saved without its preview
		}
		if err := s.putFile(ctx, key, filepath.Join(dir, filepath.Base(key))); err != nil {
			return nil, err
		}
	}
	u, err := librarydomain.NewUsage(a.ID(), workspaceID, h.Harvest.StoryID, h.Harvest.DeliverableID, h.Harvest.Revision, s.now())
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, a); err != nil {
			return err
		}
		return s.repo.RecordUsage(ctx, u)
	})
	if apperror.Is(err, apperror.Conflict) {
		// Another harvest saved it meanwhile; that one stands.
		_ = s.blobs.DeletePrefix(context.WithoutCancel(ctx), a.Prefix())
		return s.Find(ctx, workspaceID, h)
	}
	if err != nil {
		_ = s.blobs.DeletePrefix(context.WithoutCancel(ctx), a.Prefix())
		return nil, err
	}
	return a, nil
}

// Decided names what a deliverable's decided revision uses, by outside
// item and by file hash.
type Decided struct {
	SourceRefs []string
	SHA256s    []string
}

func (d Decided) uses(a *librarydomain.Asset) bool {
	if ref := a.Harvest().SourceRef; ref != "" {
		return slices.Contains(d.SourceRefs, ref)
	}
	return slices.Contains(d.SHA256s, a.SHA256())
}

// Settle decides the provisional assets saved from a deliverable's
// drafts once the deliverable is decided: those the decided revision
// uses, a person kept, or another video used stay; the rest are deleted
// with their files. It returns how many were deleted.
func (s *Service) Settle(ctx context.Context, workspaceID, deliverableID uuid.UUID, decided Decided) (int, error) {
	assets, err := s.repo.Provisional(ctx, workspaceID, deliverableID)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, a := range assets {
		uses, err := s.repo.Usages(ctx, workspaceID, a.ID())
		if err != nil {
			return deleted, err
		}
		elsewhere := slices.ContainsFunc(uses, func(u librarydomain.Usage) bool { return u.DeliverableID != deliverableID })
		if a.Settle(decided.uses(a), elsewhere, s.now()) {
			if err := s.repo.Update(ctx, a); err != nil {
				return deleted, err
			}
			continue
		}
		if err := s.repo.Delete(ctx, workspaceID, a.ID()); err != nil && !apperror.Is(err, apperror.AssetNotFound) {
			return deleted, err
		}
		if err := s.blobs.DeletePrefix(ctx, a.Prefix()); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path) //nolint:gosec // a draft's own file
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
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
