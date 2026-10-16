package production

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	storydomain "kurgu/internal/domain/story/v1"
	"kurgu/pkg/archive"
)

// Files a reviewer can edit in place, relative to the work dir.
const (
	narrationFile = "narration/narration.txt"
	metadataFile  = "metadata.json"
	articleFile   = "article.md"
	// revisionMarker records which revision a work dir holds.
	revisionMarker = ".kurgu-revision"
)

// VideoMetadata is the editable part of a video's metadata.json; other
// keys (chapters) are kept as the agent wrote them.
type VideoMetadata struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// Content is a deliverable's editable text at one revision.
type Content struct {
	Revision  int
	Article   *string
	Narration *string
	Metadata  *VideoMetadata
}

// Edit is a reviewer's change; nil fields stay as they are.
type Edit struct {
	// Revision is the revision the edit was made on; a newer one
	// refuses the edit (someone else changed it meanwhile).
	Revision  int
	Article   *string
	Narration *string
	Metadata  *VideoMetadata
}

const maxEditableBytes = 1 << 20

// Content returns the editable text of a deliverable's current revision.
func (s *Service) Content(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID) (Content, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return Content{}, err
	}
	d, err := st.Deliverable(deliverableID)
	if err != nil {
		return Content{}, err
	}
	if d.Revision() == 0 {
		return Content{}, apperror.New(apperror.NotFound, "nothing is drafted yet")
	}
	files, err := s.revisionFiles(ctx, d, narrationFile, metadataFile, articleFile)
	if err != nil {
		return Content{}, err
	}
	c := Content{Revision: d.Revision()}
	if d.IsVideo() {
		if b, ok := files[narrationFile]; ok {
			n := string(b)
			c.Narration = &n
		}
		if b, ok := files[metadataFile]; ok {
			var m VideoMetadata
			if json.Unmarshal(b, &m) == nil {
				c.Metadata = &m
			}
		}
	} else if b, ok := files[articleFile]; ok {
		a := string(b)
		c.Article = &a
	}
	return c, nil
}

func (s *Service) revisionFiles(ctx context.Context, d *storydomain.Deliverable, names ...string) (map[string][]byte, error) {
	rev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
	if err != nil {
		return nil, err
	}
	key, ok := rev.Artifacts[storydomain.ArtifactWorkdir]
	if !ok {
		return nil, apperror.New(apperror.NotFound, "this revision kept no working files")
	}
	rc, _, err := s.blobs.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return archive.ReadFiles(rc, maxEditableBytes, names...)
}

// retimeFeedback sends an edited narration back to the agent.
const retimeFeedback = "I edited the narration myself: narration/narration.txt now holds the exact text to speak. " +
	"Keep it word for word. Run the tts tool again, then re-time every scene change, caption, chapter and cue " +
	"in composition.html to the new timings, update script.md (and the chapters in metadata.json) to match, " +
	"and look at the affected frames."

// EditContent applies a reviewer's edit to a deliverable in review as a
// new revision. An article or metadata edit stays in review; an edited
// narration goes back to the agent to re-narrate and re-time the video.
func (s *Service) EditContent(ctx context.Context, workspaceID, storyID, deliverableID, by uuid.UUID, e Edit) (*storydomain.Story, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, err
	}
	d, err := st.Deliverable(deliverableID)
	if err != nil {
		return nil, err
	}
	if d.Status() != storydomain.StatusReview {
		return nil, apperror.New(apperror.InvalidTransition, "only a deliverable in review can be edited")
	}
	if e.Revision != d.Revision() {
		return nil, apperror.New(apperror.Conflict, fmt.Sprintf("revision %d is no longer the latest (now %d); reload and edit again", e.Revision, d.Revision()))
	}
	if err := checkEdit(d, e); err != nil {
		return nil, err
	}

	// Edit a fresh copy of the revision's files, not this host's work
	// dir, which may hold an older revision.
	dir, err := os.MkdirTemp("", "kurgu-edit-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := s.restore(ctx, d, dir); err != nil {
		return nil, err
	}
	changedNarration, err := applyEdit(dir, e)
	if err != nil {
		return nil, err
	}
	artifacts, err := s.archive(ctx, st, d, dir)
	if err != nil {
		return nil, err
	}
	words := wordsIn(d, dir)
	prev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
	if err != nil {
		return nil, err
	}
	var retime *storydomain.Feedback
	if changedNarration {
		fb, err := storydomain.NewFeedback([]storydomain.Note{{Text: retimeFeedback, Scope: storydomain.ScopeScript}}, by, s.now())
		if err != nil {
			return nil, err
		}
		retime = &fb
		delete(artifacts, storydomain.ArtifactNarration) // the old audio no longer matches
	} else if d.IsVideo() {
		// The picture is unchanged: keep showing the rendered preview.
		for _, name := range []string{storydomain.ArtifactPreview, storydomain.ArtifactPoster} {
			if key, ok := prev.Artifacts[name]; ok {
				artifacts[name] = key
			}
		}
	}
	out, err := s.mutate(ctx, storyID, func(ctx context.Context, st *storydomain.Story) error {
		d, err := st.Deliverable(deliverableID)
		if err != nil {
			return err
		}
		if d.Revision() != e.Revision {
			return apperror.New(apperror.Conflict, "the deliverable changed while saving; reload and edit again")
		}
		rev, err := st.Edited(deliverableID, maps.Clone(artifacts), by, retime, words, s.now())
		if err != nil {
			return err
		}
		if err := s.stories.PutRevision(ctx, rev); err != nil {
			return err
		}
		d, _ = st.Deliverable(deliverableID)
		return s.enqueueStep(ctx, st, d)
	})
	if err != nil {
		return nil, err
	}
	// This host's copy is now older than the revision; the next step
	// restores it.
	_ = os.RemoveAll(s.workDir(d))
	return out, nil
}

func checkEdit(d *storydomain.Deliverable, e Edit) error {
	invalid := func(m string) error { return apperror.New(apperror.ValidationFailed, m) }
	if e.Article == nil && e.Narration == nil && e.Metadata == nil {
		return invalid("nothing to save")
	}
	if d.IsVideo() {
		if e.Article != nil {
			return invalid("a video has no article text")
		}
		if e.Narration != nil {
			n := strings.TrimSpace(*e.Narration)
			if n == "" || len(n) > maxEditableBytes || !utf8.ValidString(n) {
				return invalid("the narration must be 1 character to 1 MB of text")
			}
		}
		if m := e.Metadata; m != nil {
			switch {
			case strings.TrimSpace(m.Title) == "" || utf8.RuneCountInString(m.Title) > 100:
				return invalid("the title must be 1–100 characters")
			case strings.ContainsAny(m.Title+m.Description, "<>"):
				return invalid("YouTube does not accept < or > in titles and descriptions")
			case utf8.RuneCountInString(m.Description) > 5000:
				return invalid("the description must be at most 5000 characters")
			case utf8.RuneCountInString(strings.Join(m.Tags, ",")) > 500:
				return invalid("the tags must be at most 500 characters in total")
			}
		}
		return nil
	}
	if e.Narration != nil || e.Metadata != nil {
		return invalid("an article has only its article text")
	}
	if e.Article == nil {
		return invalid("nothing to save")
	}
	if len(*e.Article) > maxEditableBytes {
		return invalid("the article is larger than 1 MB")
	}
	if _, err := ParseArticle([]byte(*e.Article)); err != nil {
		return apperror.New(apperror.ValidationFailed, err.Error())
	}
	return nil
}

// applyEdit writes e into dir and reports whether the narration changed.
func applyEdit(dir string, e Edit) (bool, error) {
	write := func(rel string, b []byte) error {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return err
		}
		return os.WriteFile(p, b, 0o600)
	}
	if e.Article != nil {
		if err := write(articleFile, []byte(normalizeText(*e.Article))); err != nil {
			return false, err
		}
	}
	changed := false
	if e.Narration != nil {
		next := normalizeText(*e.Narration)
		old, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(narrationFile))) //nolint:gosec // our temp dir
		if strings.TrimSpace(string(old)) != strings.TrimSpace(next) {
			changed = true
			if err := write(narrationFile, []byte(next)); err != nil {
				return false, err
			}
		}
	}
	if m := e.Metadata; m != nil {
		doc := map[string]any{}
		if old, err := os.ReadFile(filepath.Join(dir, metadataFile)); err == nil { //nolint:gosec // our temp dir
			_ = json.Unmarshal(old, &doc)
		}
		tags := []string{}
		for _, t := range m.Tags {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		doc["title"], doc["description"], doc["tags"] = strings.TrimSpace(m.Title), strings.TrimSpace(m.Description), tags
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doc); err != nil {
			return false, err
		}
		if err := write(metadataFile, b.Bytes()); err != nil {
			return false, err
		}
	}
	return changed, nil
}

// normalizeText uses \n line ends and one final newline, as editors in
// browsers send \r\n.
func normalizeText(s string) string {
	return strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n") + "\n"
}
