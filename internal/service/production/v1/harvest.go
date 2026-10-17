package production

import (
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

	"kurgu/internal/agenttool"
	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	librarydomain "kurgu/internal/domain/library/v1"
	storydomain "kurgu/internal/domain/story/v1"
	librarysvc "kurgu/internal/service/library/v1"
	"kurgu/pkg/archive"
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
	dir, err := os.MkdirTemp("", "kurgu-harvest-*")
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
	items := s.harvestables(ctx, st, p, dir)
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
func (s *Service) harvestables(ctx context.Context, st *storydomain.Story, p HarvestPayload, dir string) []librarysvc.Harvestable {
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
		preview := filepath.Join(dir, "library-preview.wav")
		if _, err := s.renderer.Music(ctx, dir, scorePreviewSeconds, preview); err == nil {
			item.Preview = preview
		} else {
			s.log.WarnContext(ctx, "cannot render a score's library preview", slog.Any("error", err))
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
