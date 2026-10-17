package production

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"ekin/internal/agenttool"
	"ekin/internal/apperror"
	jobdomain "ekin/internal/domain/job/v1"
	librarydomain "ekin/internal/domain/library/v1"
	storydomain "ekin/internal/domain/story/v1"
	librarysvc "ekin/internal/service/library/v1"
	"ekin/pkg/archive"
)

// KindHarvest saves what a video revision used to the library, for the
// kinds the workspace saves; with Settle it then decides what the
// deliverable's drafts saved, now that it is decided.
const KindHarvest = "library.harvest"

// HarvestPayload is the library.harvest job payload.
type HarvestPayload struct {
	WorkspaceID   uuid.UUID `json:"workspaceId"`
	StoryID       uuid.UUID `json:"storyId"`
	DeliverableID uuid.UUID `json:"deliverableId"`
	Revision      int       `json:"revision"`
	Settle        bool      `json:"settle,omitempty"`
}

// pexelsLicence is the licence line Pexels stock is credited with.
const pexelsLicence = "Pexels License"

// scorePreviewSeconds is how much of a composed score is rendered for its
// preview.
const scorePreviewSeconds = 30

// enqueueHarvest queues a library.harvest job for d's revision: to save
// it (when the workspace saves anything) or to settle the deliverable. A
// short is skipped: its media is its long video's. Call it inside the
// transaction that finished or decided the revision.
func (s *Service) enqueueHarvest(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, revision int, settle bool) error {
	if s.library == nil || !d.IsVideo() || d.Clip() != nil || revision < 1 {
		return nil
	}
	step := "settle"
	if !settle {
		collect, err := s.library.Collect(ctx, st.WorkspaceID())
		if err != nil {
			return err
		}
		if !collect.Any() {
			return nil
		}
		step = "save"
	}
	payload, _ := json.Marshal(HarvestPayload{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), DeliverableID: d.ID(), Revision: revision, Settle: settle})
	_, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{
		WorkspaceID: st.WorkspaceID(), Queue: jobdomain.QueueIO, Kind: KindHarvest, Payload: payload,
		UniqueKey: fmt.Sprintf("%s/%s/%s/r%d", KindHarvest, step, d.ID(), revision), MaxAttempts: 3, TraceID: st.TraceID(),
	})
	return err
}

// Harvest runs a library.harvest job: it unpacks the revision, saves the
// Pexels stock its composition uses and its composed score (each kind as
// the workspace saves it), and when settling keeps only what that
// revision uses of what the deliverable's drafts saved.
func (s *Service) Harvest(ctx context.Context, p HarvestPayload) error {
	if s.library == nil {
		return nil
	}
	st, err := s.stories.GetByID(ctx, p.StoryID)
	if apperror.Is(err, apperror.StoryNotFound) {
		if !p.Settle {
			return nil
		}
		// The story is gone: nothing of its drafts is wanted for it.
		_, err := s.library.Settle(ctx, p.WorkspaceID, p.DeliverableID, librarysvc.Decided{})
		return err
	}
	if err != nil {
		return err
	}
	ws := st.WorkspaceID()
	dir, err := os.MkdirTemp("", "ekin-harvest-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := s.unpackRevision(ctx, p.DeliverableID, p.Revision, dir); err != nil {
		return err
	}
	collect, err := s.library.Collect(ctx, ws)
	if err != nil {
		return err
	}
	items := s.harvestables(ctx, st, p, dir, true)
	var decided librarysvc.Decided
	for _, h := range items {
		if h.Harvest.SourceRef != "" {
			decided.SourceRefs = append(decided.SourceRefs, h.Harvest.SourceRef)
		} else if sum, err := fileHash(h.Path); err == nil {
			decided.SHA256s = append(decided.SHA256s, sum)
		}
		if !collect.Collects(h.Kind) {
			continue
		}
		if _, err := s.library.Save(ctx, ws, uuid.Nil, h, false); err != nil {
			if apperror.Is(err, apperror.ValidationFailed) {
				// A file the library would not take (a clip past its length
				// limit): skipping it fails nothing.
				s.log.WarnContext(ctx, "library cannot keep a draft's file", slog.String("file", filepath.Base(h.Path)), slog.Any("error", err))
				continue
			}
			return err
		}
	}
	if !p.Settle {
		return nil
	}
	_, err = s.library.Settle(ctx, ws, p.DeliverableID, decided)
	return err
}

// unpackRevision restores a revision's archived work dir into dir.
func (s *Service) unpackRevision(ctx context.Context, deliverableID uuid.UUID, number int, dir string) error {
	rev, err := s.stories.Revision(ctx, deliverableID, number)
	if err != nil {
		return err
	}
	key, ok := rev.Artifacts[storydomain.ArtifactWorkdir]
	if !ok {
		return nil
	}
	rc, _, err := s.blobs.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("open revision work dir: %w", err)
	}
	defer func() { _ = rc.Close() }()
	return archive.Untar(rc, dir)
}

// harvestables lists what a revision in dir could give the library: the
// Pexels stock its composition uses and its composed score.
func (s *Service) harvestables(ctx context.Context, st *storydomain.Story, p HarvestPayload, dir string, preview bool) []librarysvc.Harvestable {
	from := librarydomain.Harvest{StoryID: st.ID(), DeliverableID: p.DeliverableID, Revision: p.Revision}
	var out []librarysvc.Harvestable
	for _, c := range agenttool.UsedStock(dir) {
		h := from
		h.SourceRef = "pexels:" + c.Kind + ":" + c.ExternalID
		h.SourceURL, h.OriginalURL = c.URL, c.OriginalURL
		var tags []string
		if q := strings.TrimSpace(c.Query); q != "" {
			tags = []string{q}
		}
		out = append(out, librarysvc.Harvestable{
			Kind: librarydomain.Kind(c.Kind), Origin: librarydomain.OriginPexels, Harvest: h,
			Details: librarydomain.Details{
				Title: stockTitle(c), Description: c.Alt, Tags: tags,
				Credit: librarydomain.Credit{Author: c.Author, URL: c.URL, Licence: pexelsLicence},
			},
			Path: filepath.Join(dir, c.File),
		})
	}
	if score := filepath.Join(dir, "music.js"); fileExists(score) {
		meta := agenttool.ReadMusicMeta(dir)
		h := from
		h.DerivedFrom = libraryScore(dir)
		title := meta.Title
		if strings.TrimSpace(title) == "" {
			title = "Score for " + st.Idea().Title
		}
		item := librarysvc.Harvestable{
			Kind: librarydomain.KindMusic, Origin: librarydomain.OriginGenerated, Harvest: h,
			Details: librarydomain.Details{Title: truncate(title, librarydomain.MaxTitleLength), Description: meta.Description,
				Mood: truncate(meta.Mood, librarydomain.MaxMoodLength), Tempo: max(0, min(meta.Tempo, librarydomain.MaxTempo))},
			Path: score,
		}
		if preview {
			wav := filepath.Join(dir, "library-preview.wav")
			if _, err := s.renderer.Music(ctx, dir, scorePreviewSeconds, wav); err == nil {
				item.Preview = wav
			} else {
				s.log.WarnContext(ctx, "cannot render a score's library preview", slog.Any("error", err))
			}
		}
		out = append(out, item)
	}
	return out
}

// libraryScore is the library score a draft took (and may have adapted),
// uuid.Nil when none.
func libraryScore(dir string) uuid.UUID {
	for _, c := range agenttool.ReadCredits(dir) {
		if c.Library != "" && c.Kind == "music" && c.File == "music.js" {
			id, _ := uuid.Parse(c.Library)
			return id
		}
	}
	return uuid.Nil
}

// stockTitle names a stock item: its alt text, its search, or its id.
func stockTitle(c agenttool.Credit) string {
	for _, t := range []string{c.Alt, c.Query} {
		if t = strings.TrimSpace(t); t != "" {
			r, size := utf8.DecodeRuneInString(t)
			return truncate(string(unicode.ToUpper(r))+t[size:], librarydomain.MaxTitleLength)
		}
	}
	return "Pexels " + c.Kind + " " + c.ExternalID
}

// truncate cuts s to at most n runes.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func fileHash(p string) (string, error) {
	raw, err := os.ReadFile(p) //nolint:gosec // a revision's own file
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// RevisionMedia is a stock item or score a revision uses, and the
// library asset it is (or came from), if any.
type RevisionMedia struct {
	// Key names it within the revision: its outside item
	// ("pexels:video:1"), "music.js" for a composed score, or
	// "library:<id>" for an item taken from the library.
	Key    string
	Kind   librarydomain.Kind
	Source string // pexels, composed or library
	Title  string
	Author string
	Asset  *librarydomain.Asset
}

// scoreKey names a revision's composed score.
const scoreKey = "music.js"

// Media lists what a video revision uses: its Pexels stock and composed
// score, which a person may save to the library, and what it took from
// the library.
func (s *Service) Media(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, number int) ([]RevisionMedia, error) {
	st, dir, cleanup, err := s.openRevision(ctx, workspaceID, storyID, deliverableID, number)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	var out []RevisionMedia
	for _, h := range s.harvestables(ctx, st, HarvestPayload{StoryID: storyID, DeliverableID: deliverableID, Revision: number}, dir, false) {
		m := RevisionMedia{Key: cmp.Or(h.Harvest.SourceRef, scoreKey), Kind: h.Kind, Source: "pexels", Title: h.Details.Title,
			Author: h.Details.Credit.Author}
		if h.Origin == librarydomain.OriginGenerated {
			m.Source = "composed"
		}
		if m.Asset, err = s.library.Find(ctx, workspaceID, h); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	for _, raw := range agenttool.LibraryIDs(dir) {
		assetID, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		a, err := s.library.Get(ctx, workspaceID, assetID)
		if apperror.Is(err, apperror.AssetNotFound) {
			continue // deleted since
		}
		if err != nil {
			return nil, err
		}
		out = append(out, RevisionMedia{Key: "library:" + raw, Kind: a.Kind(), Source: "library", Title: a.Title(),
			Author: a.Credit().Author, Asset: a})
	}
	return out, nil
}

// SaveMedia saves one of a revision's stock items or its score to the
// library, kept whatever the workspace's saving switches say.
func (s *Service) SaveMedia(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, number int, key string,
	actorID uuid.UUID) (*librarydomain.Asset, error) {
	st, dir, cleanup, err := s.openRevision(ctx, workspaceID, storyID, deliverableID, number)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	for _, h := range s.harvestables(ctx, st, HarvestPayload{StoryID: storyID, DeliverableID: deliverableID, Revision: number}, dir, true) {
		if cmp.Or(h.Harvest.SourceRef, scoreKey) == key {
			return s.library.Save(ctx, workspaceID, actorID, h, true)
		}
	}
	return nil, apperror.New(apperror.NotFound, "the revision uses no such stock or music")
}

// openRevision unpacks a video revision of a story in the workspace into
// a temporary directory, which cleanup removes.
func (s *Service) openRevision(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, number int) (*storydomain.Story, string, func(), error) {
	if s.library == nil {
		return nil, "", nil, apperror.New(apperror.NotFound, "the library is not available")
	}
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, "", nil, err
	}
	d, err := st.Deliverable(deliverableID)
	if err != nil {
		return nil, "", nil, err
	}
	if !d.IsVideo() || number < 1 || number > d.Revision() {
		return nil, "", nil, apperror.New(apperror.NotFound, "no such video revision")
	}
	dir, err := os.MkdirTemp("", "ekin-revision-*")
	if err != nil {
		return nil, "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := s.unpackRevision(ctx, deliverableID, number, dir); err != nil {
		cleanup()
		return nil, "", nil, err
	}
	return st, dir, cleanup, nil
}
