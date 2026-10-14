package production

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	storydomain "kurgu/internal/domain/story/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	"kurgu/pkg/id"
)

// Render runs a deliverable.render job: the preview (for review) or the
// final full-resolution render (after approval).
func (s *Service) Render(ctx context.Context, p DeliverablePayload) error {
	st, err := s.stories.GetByID(ctx, p.StoryID)
	if apperror.Is(err, apperror.StoryNotFound) {
		return jobdomain.Permanent(err)
	}
	if err != nil {
		return err
	}
	d, err := st.Deliverable(p.DeliverableID)
	if err != nil {
		return jobdomain.Permanent(err)
	}
	want := storydomain.StatusRendering
	if p.Final {
		want = storydomain.StatusFinalizing
	}
	if d.Status() != want {
		return nil // superseded (rejected, changed, retried)
	}
	keys, err := s.renderVideo(ctx, st, d, p.Final)
	if err != nil {
		s.log.ErrorContext(ctx, "render failed", slog.String("deliverable", d.ID().String()), slog.Any("error", err))
		s.fail(ctx, st, d, err)
		return nil
	}
	st, err = s.mutate(ctx, p.StoryID, func(ctx context.Context, st *storydomain.Story) error {
		rev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
		if err != nil {
			return err
		}
		for k, v := range keys {
			rev.Artifacts[k] = v
		}
		if err := s.stories.PutRevision(ctx, rev); err != nil {
			return err
		}
		return st.Rendered(d.ID(), s.now())
	})
	if err != nil {
		return err
	}
	d, _ = st.Deliverable(d.ID())
	switch d.Status() {
	case storydomain.StatusReview:
		s.notify(ctx, Event{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), Kind: "review", Title: st.Idea().Title,
			Detail: fmt.Sprintf("video revision %d is ready for review", d.Revision())})
	case storydomain.StatusReady:
		return s.afterReady(ctx, st, d)
	}
	return nil
}

// afterReady publishes a ready deliverable when the workspace has a
// target; otherwise it releases what was waiting on it right away.
func (s *Service) afterReady(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable) error {
	queued, err := s.autoPublish(ctx, st, d)
	if err != nil || queued {
		return err
	}
	return s.releaseDerived(ctx, st.ID(), d.ID())
}

func (s *Service) releaseDerived(ctx context.Context, storyID, source uuid.UUID) error {
	_, err := s.mutate(ctx, storyID, func(ctx context.Context, st *storydomain.Story) error {
		queued, err := st.ReleaseDerived(source, s.now())
		if err != nil {
			return err
		}
		for _, q := range queued {
			if err := s.enqueueStep(ctx, st, q); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (s *Service) renderVideo(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, final bool) (map[string]string, error) {
	dir := s.workDir(d)
	if err := s.ensureWorkDir(ctx, d, dir); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "kurgu-render-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	scale := s.cfg.PreviewScale
	name := storydomain.ArtifactPreview
	if final {
		scale, name = s.cfg.FinalScale, storydomain.ArtifactFinal
	}
	silent := filepath.Join(tmp, "silent.mp4")
	meta, err := s.renderer.Video(ctx, dir, "composition.html", scale, silent)
	if err != nil {
		return nil, err
	}
	music := ""
	if _, err := os.Stat(filepath.Join(dir, "music.js")); err == nil {
		music = filepath.Join(tmp, "music.wav")
		if _, err := s.renderer.Music(ctx, dir, meta.Duration, music); err != nil {
			return nil, err
		}
	}
	out := filepath.Join(tmp, name)
	if err := s.renderer.Mix(ctx, silent, filepath.Join(dir, "narration", "speech.wav"), music, meta.Duration, out); err != nil {
		return nil, err
	}
	poster := filepath.Join(tmp, storydomain.ArtifactPoster)
	if err := s.renderer.Poster(ctx, out, math.Min(1.5, meta.Duration/3), poster); err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("ws/%s/stories/%s/%s/r%d/", st.WorkspaceID(), st.ID(), d.ID(), d.Revision())
	keys := map[string]string{}
	for artifact, path := range map[string]string{name: out, storydomain.ArtifactPoster: poster} {
		if err := s.putFile(ctx, prefix+artifact, path); err != nil {
			return nil, err
		}
		keys[artifact] = prefix + artifact
	}
	return keys, nil
}

// Approve accepts a deliverable in review and queues what follows.
func (s *Service) Approve(ctx context.Context, workspaceID, storyID, deliverableID, actorID uuid.UUID) (*storydomain.Story, error) {
	var queued []*storydomain.Deliverable
	st, err := s.change(ctx, workspaceID, storyID, func(ctx context.Context, st *storydomain.Story) error {
		var err error
		queued, err = st.Approve(deliverableID, actorID, id.New, s.now())
		if err != nil {
			return err
		}
		d, _ := st.Deliverable(deliverableID)
		if err := s.enqueueStep(ctx, st, d); err != nil {
			return err
		}
		for _, q := range queued {
			if err := s.enqueueStep(ctx, st, q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if d, _ := st.Deliverable(deliverableID); d.Status() == storydomain.StatusReady {
		if err := s.afterReady(ctx, st, d); err != nil {
			return st, err
		}
		return s.stories.Get(ctx, workspaceID, storyID)
	}
	return st, nil
}

// RequestChanges sends a deliverable back to the agent with feedback.
func (s *Service) RequestChanges(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, fb storydomain.Feedback) (*storydomain.Story, error) {
	return s.change(ctx, workspaceID, storyID, func(ctx context.Context, st *storydomain.Story) error {
		if err := st.RequestChanges(deliverableID, fb, s.now()); err != nil {
			return err
		}
		d, _ := st.Deliverable(deliverableID)
		return s.enqueueStep(ctx, st, d)
	})
}

// Reject drops a deliverable.
func (s *Service) Reject(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID) (*storydomain.Story, error) {
	return s.change(ctx, workspaceID, storyID, func(_ context.Context, st *storydomain.Story) error {
		return st.Reject(deliverableID, s.now())
	})
}

// Retry resumes a failed deliverable.
func (s *Service) Retry(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID) (*storydomain.Story, error) {
	var resumed storydomain.Status
	st, err := s.change(ctx, workspaceID, storyID, func(ctx context.Context, st *storydomain.Story) error {
		var err error
		if resumed, err = st.Retry(deliverableID, s.now()); err != nil {
			return err
		}
		d, _ := st.Deliverable(deliverableID)
		return s.enqueueStep(ctx, st, d)
	})
	if err == nil && resumed == storydomain.StatusReady {
		d, _ := st.Deliverable(deliverableID)
		err = s.PublishNow(ctx, st, d)
	}
	return st, err
}

// Publish queues publication of a ready deliverable.
func (s *Service) Publish(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID) (*storydomain.Story, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, err
	}
	d, err := st.Deliverable(deliverableID)
	if err != nil {
		return nil, err
	}
	if d.Status() != storydomain.StatusReady {
		return nil, apperror.New(apperror.InvalidTransition, "only an approved, rendered deliverable can be published")
	}
	pub, ok := s.publishers[d.Kind()]
	if !ok {
		return nil, apperror.New(apperror.ValidationFailed, "no publisher for "+string(d.Kind())+" is configured")
	}
	if ready, err := pub.Ready(ctx, workspaceID); err != nil || !ready {
		if err != nil {
			return nil, err
		}
		return nil, apperror.New(apperror.ValidationFailed, "connect a "+publisherTarget(d)+" in workspace settings first")
	}
	return st, s.PublishNow(ctx, st, d)
}

// PublishNow queues the publish job.
func (s *Service) PublishNow(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable) error {
	return s.enqueuePublish(ctx, st, d)
}

func publisherTarget(d *storydomain.Deliverable) string {
	if d.IsVideo() {
		return "YouTube channel"
	}
	return "blog target"
}

// autoPublish queues publication when the workspace has a target and
// reports whether it did.
func (s *Service) autoPublish(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable) (bool, error) {
	pub, ok := s.publishers[d.Kind()]
	if !ok {
		return false, nil
	}
	ready, err := pub.Ready(ctx, st.WorkspaceID())
	if err != nil || !ready {
		return false, err
	}
	return true, s.enqueuePublish(ctx, st, d)
}

// PublishRequest is what a publisher gets.
type PublishRequest struct {
	WorkspaceID uuid.UUID
	Story       *storydomain.Story
	Deliverable *storydomain.Deliverable
	Revision    storydomain.Revision
	// Open returns a stored artifact of this revision by name.
	Open func(ctx context.Context, name string) (io.ReadCloser, int64, error)
	// Lead is the lead's publication, for an article built around a video.
	Lead *storydomain.Publication
	// Translations pairs this article with its other languages (by slug).
	TranslationKey string
}

// PublishJob runs a deliverable.publish job.
func (s *Service) PublishJob(ctx context.Context, p DeliverablePayload) error {
	st, err := s.mutate(ctx, p.StoryID, func(_ context.Context, st *storydomain.Story) error {
		return st.StartPublishing(p.DeliverableID, s.now())
	})
	if apperror.Is(err, apperror.InvalidTransition) || apperror.Is(err, apperror.StoryNotFound) {
		return jobdomain.Permanent(err)
	}
	if err != nil {
		return err
	}
	d, _ := st.Deliverable(p.DeliverableID)
	pub, ok := s.publishers[d.Kind()]
	if !ok {
		s.fail(ctx, st, d, errors.New("no publisher configured"))
		return nil
	}
	rev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
	if err != nil {
		return err
	}
	req := PublishRequest{
		WorkspaceID: st.WorkspaceID(), Story: st, Deliverable: d, Revision: rev,
		Open: func(ctx context.Context, name string) (io.ReadCloser, int64, error) {
			key, ok := rev.Artifacts[name]
			if !ok {
				return nil, 0, fmt.Errorf("revision has no %s", name)
			}
			rc, info, err := s.blobs.Open(ctx, key)
			return rc, info.Size, err
		},
		TranslationKey: st.ID().String()[:8],
	}
	if src, err := st.Deliverable(d.SourceID()); err == nil && src.Publication() != nil {
		req.Lead = src.Publication()
	}
	if lead := st.Lead(); lead != nil && lead.ID() != d.ID() && lead.Publication() != nil && req.Lead == nil {
		req.Lead = lead.Publication()
	}
	publication, err := pub.Publish(ctx, req)
	if err != nil {
		s.log.ErrorContext(ctx, "publish failed", slog.Any("error", err))
		s.fail(ctx, st, d, err)
		return nil
	}
	st, err = s.mutate(ctx, p.StoryID, func(_ context.Context, st *storydomain.Story) error {
		return st.Published(p.DeliverableID, publication, s.now())
	})
	if err != nil {
		return err
	}
	s.notify(ctx, Event{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), Kind: "published", Title: st.Idea().Title,
		Detail: fmt.Sprintf("%s published: %s", d.Kind(), publication.URL)})
	// Derived deliverables can now be drafted around the live lead.
	return s.releaseDerived(ctx, st.ID(), d.ID())
}

// change loads a story within the workspace, applies fn and saves.
func (s *Service) change(ctx context.Context, workspaceID, storyID uuid.UUID, fn func(ctx context.Context, st *storydomain.Story) error) (*storydomain.Story, error) {
	if _, err := s.stories.Get(ctx, workspaceID, storyID); err != nil {
		return nil, err
	}
	return s.mutate(ctx, storyID, fn)
}

// Get returns a story.
func (s *Service) Get(ctx context.Context, workspaceID, storyID uuid.UUID) (*storydomain.Story, error) {
	return s.stories.Get(ctx, workspaceID, storyID)
}

// Filter narrows a story listing.
type Filter = storyrepo.Filter

// List returns stories newest first.
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*storydomain.Story, error) {
	return s.stories.List(ctx, workspaceID, f)
}

// Revisions lists a deliverable's revisions.
func (s *Service) Revisions(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID) ([]storydomain.Revision, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, err
	}
	if _, err := st.Deliverable(deliverableID); err != nil {
		return nil, err
	}
	return s.stories.Revisions(ctx, deliverableID)
}

// Delete removes a story, its artifacts and working directories.
func (s *Service) Delete(ctx context.Context, workspaceID, storyID uuid.UUID) error {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return err
	}
	for _, d := range st.Deliverables() {
		switch d.Status() {
		case storydomain.StatusDrafting, storydomain.StatusRendering, storydomain.StatusFinalizing, storydomain.StatusPublishing:
			return apperror.New(apperror.Conflict, "the story is being worked on; wait for the current step to finish")
		}
	}
	if err := s.stories.Delete(ctx, workspaceID, storyID); err != nil {
		return err
	}
	for _, d := range st.Deliverables() {
		_ = os.RemoveAll(s.workDir(d))
	}
	return s.blobs.DeletePrefix(ctx, fmt.Sprintf("ws/%s/stories/%s", workspaceID, storyID))
}

// Artifact opens a stored artifact of a revision.
func (s *Service) Artifact(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, revision int, name string) (io.ReadCloser, int64, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, 0, err
	}
	if _, err := st.Deliverable(deliverableID); err != nil {
		return nil, 0, err
	}
	rev, err := s.stories.Revision(ctx, deliverableID, revision)
	if err != nil {
		return nil, 0, err
	}
	key, ok := rev.Artifacts[name]
	if !ok {
		return nil, 0, apperror.New(apperror.NotFound, "the revision has no "+name)
	}
	rc, info, err := s.blobs.Open(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	return rc, info.Size, nil
}
