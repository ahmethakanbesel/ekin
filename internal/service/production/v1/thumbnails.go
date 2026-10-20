package production

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/agenttool"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
)

// PickThumbnail picks variant n of a video's current revision as the
// thumbnail it goes out with; 0 goes back to the recommended one.
func (s *Service) PickThumbnail(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, n int) (*storydomain.Story, error) {
	return s.change(ctx, workspaceID, storyID, func(ctx context.Context, st *storydomain.Story) error {
		d, err := st.Deliverable(deliverableID)
		if err != nil {
			return err
		}
		if d.Revision() == 0 {
			return apperror.New(apperror.ValidationFailed, "nothing is drafted yet")
		}
		rev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
		if err != nil {
			return err
		}
		return st.PickThumbnail(deliverableID, n, rev.Thumbnails, s.now())
	})
}

// EditThumbnail replaces the words of variant n of a video in review, on
// its revision `revision` (a newer one refuses the edit), as a new
// revision whose thumbnails alone are drawn again. No agent is involved.
func (s *Service) EditThumbnail(ctx context.Context, workspaceID, storyID, deliverableID, by uuid.UUID, revision, n int, text string) (*storydomain.Story, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, err
	}
	d, err := st.Deliverable(deliverableID)
	if err != nil {
		return nil, err
	}
	if d.Status() != storydomain.StatusReview {
		return nil, apperror.New(apperror.InvalidTransition, "only a video in review can be edited")
	}
	if revision != d.Revision() {
		return nil, apperror.New(apperror.Conflict, fmt.Sprintf("revision %d is no longer the latest (now %d); reload and edit again", revision, d.Revision()))
	}
	prev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
	if err != nil {
		return nil, err
	}
	if prev.Thumbnails.IsZero() {
		return nil, apperror.New(apperror.ValidationFailed, "this revision has no thumbnails to edit")
	}
	thumbs, err := prev.Thumbnails.WithText(n, text)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "ekin-edit-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := s.restore(ctx, d, dir); err != nil {
		return nil, err
	}
	if err := writeThumbnailText(filepath.Join(dir, agenttool.ThumbnailsFile), thumbs); err != nil {
		return nil, err
	}
	artifacts, err := s.archive(ctx, st, d, dir)
	if err != nil {
		return nil, err
	}
	// The video is unchanged and keeps playing while the thumbnails are
	// drawn again; until then the old images stand in.
	for name, key := range prev.Artifacts {
		if name == storydomain.ArtifactPreview || name == storydomain.ArtifactPoster || storydomain.IsThumbnailArtifact(name) {
			artifacts[name] = key
		}
	}
	out, err := s.mutate(ctx, storyID, func(ctx context.Context, st *storydomain.Story) error {
		d, err := st.Deliverable(deliverableID)
		if err != nil {
			return err
		}
		if d.Revision() != revision {
			return apperror.New(apperror.Conflict, "the deliverable changed while saving; reload and edit again")
		}
		rev, err := st.EditedThumbnails(deliverableID, maps.Clone(artifacts), by, thumbs, s.now())
		if err != nil {
			return err
		}
		rev.Preview = prev.Preview
		if err := s.stories.PutRevision(ctx, rev); err != nil {
			return err
		}
		d, _ = st.Deliverable(deliverableID)
		return s.enqueueStep(ctx, st, d)
	})
	if err != nil {
		// Nothing points at this attempt's files.
		if key := artifacts[storydomain.ArtifactWorkdir]; key != "" {
			_ = s.blobs.DeletePrefix(context.WithoutCancel(ctx), path.Dir(key)+"/")
		}
		return nil, err
	}
	// This host's copy is now older than the revision; the render
	// restores it.
	_ = os.RemoveAll(s.workDir(d))
	return out, nil
}

// writeThumbnailText puts t's words into the thumbnails.json at p,
// keeping every other key the agent wrote.
func writeThumbnailText(p string, t storydomain.Thumbnails) error {
	raw, err := os.ReadFile(p) //nolint:gosec // our own copy of the work dir
	if err != nil {
		return apperror.New(apperror.NotFound, "the revision kept no "+agenttool.ThumbnailsFile)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return apperror.Wrap(apperror.InternalError, err, agenttool.ThumbnailsFile+" is unreadable")
	}
	variants, _ := doc["variants"].([]any)
	for _, raw := range variants {
		v, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		n, _ := v["n"].(float64)
		for _, want := range t.Variants {
			if int(n) == want.N {
				v["text"] = want.Text
			}
		}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// maxThumbnailsZip bounds the variants' zip: four JPEGs under YouTube's
// 2 MB each.
const maxThumbnailsZip = 16 << 20

// ThumbnailsZip packs the thumbnail variants of a revision of a video
// into a zip, named by variant number and concept.
func (s *Service) ThumbnailsZip(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, revision int) ([]byte, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, err
	}
	if _, err := st.Deliverable(deliverableID); err != nil {
		return nil, err
	}
	rev, err := s.stories.Revision(ctx, deliverableID, revision)
	if err != nil {
		return nil, err
	}
	if rev.Thumbnails.IsZero() {
		return nil, apperror.New(apperror.NotFound, "the revision has no thumbnails")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, v := range rev.Thumbnails.Variants {
		key, ok := rev.Artifacts[storydomain.ThumbnailArtifact(v.N)]
		if !ok {
			continue
		}
		rc, _, err := s.blobs.Open(ctx, key)
		if err != nil {
			return nil, err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("thumbnail-%d-%s.jpg", v.N, slug(v.Concept)), Method: zip.Store})
		if err == nil {
			_, err = io.Copy(w, io.LimitReader(rc, maxThumbnailsZip))
		}
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// slug keeps a concept's ASCII letters, digits and dashes for a file name.
func slug(s string) string {
	var b []byte
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b = append(b, byte(r))
		case r >= 'A' && r <= 'Z':
			b = append(b, byte(r-'A'+'a'))
		case len(b) > 0 && b[len(b)-1] != '-':
			b = append(b, '-')
		}
	}
	for len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	if len(b) == 0 {
		return "variant"
	}
	return string(b)
}
