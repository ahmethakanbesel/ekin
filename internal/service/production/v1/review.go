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
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/render"
	"kurgu/internal/agenttool"
	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	storydomain "kurgu/internal/domain/story/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	"kurgu/pkg/id"
)

// RenderTask is a render job: with the working directory it is
// everything RunRender needs, so a remote worker can run it.
type RenderTask struct {
	Payload  DeliverablePayload `json:"payload"`
	Revision int                `json:"revision"`
	// Scale is the render scale from the story's preview or final
	// quality; 0 takes the host profile's.
	Scale float64 `json:"scale,omitempty"`
}

// Render runs a deliverable.render job here: the preview (for review)
// or the final full-resolution render (after approval).
func (s *Service) Render(ctx context.Context, p DeliverablePayload) error {
	task, dir, err := s.BeginRender(ctx, p)
	if err != nil || dir == "" {
		return err
	}
	out, err := os.MkdirTemp("", "kurgu-render-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(out) }()
	files, err := s.RunRender(ctx, task, dir, out)
	if err != nil {
		return s.stepFailed(ctx, p, err)
	}
	return s.FinishRender(ctx, task, files)
}

// BeginRender checks the deliverable still wants this render and makes
// sure its working directory is here. An empty dir means nothing to do.
func (s *Service) BeginRender(ctx context.Context, p DeliverablePayload) (RenderTask, string, error) {
	st, err := s.stories.GetByID(ctx, p.StoryID)
	if apperror.Is(err, apperror.StoryNotFound) {
		return RenderTask{}, "", jobdomain.Permanent(err)
	}
	if err != nil {
		return RenderTask{}, "", err
	}
	d, err := st.Deliverable(p.DeliverableID)
	if err != nil {
		return RenderTask{}, "", jobdomain.Permanent(err)
	}
	want := storydomain.StatusRendering
	if p.Final {
		want = storydomain.StatusFinalizing
	}
	if d.Status() != want {
		return RenderTask{}, "", nil // superseded (rejected, changed, retried)
	}
	dir := s.workDir(d)
	if err := s.ensureWorkDir(ctx, d, dir); err != nil {
		return RenderTask{}, "", s.stepFailed(ctx, p, err)
	}
	task := RenderTask{Payload: p, Revision: d.Revision(), Scale: st.Brief().Production.PreviewQuality.Scale()}
	if p.Final {
		task.Scale = st.Brief().Production.FinalQuality.Scale()
	}
	return task, dir, nil
}

// RunRender renders dir into outDir and returns the artifact files by
// name. It touches no database.
func (s *Service) RunRender(ctx context.Context, task RenderTask, dir, outDir string) (map[string]string, error) {
	rs := s.renderSettings(ctx)
	opts := render.VideoOptions{Scale: rs.PreviewScale, Duty: rs.Duty, Chunks: rs.Chunks}
	name := storydomain.ArtifactPreview
	if task.Payload.Final {
		opts.Scale, name = rs.FinalScale, storydomain.ArtifactFinal
	}
	if task.Scale > 0 {
		opts.Scale = task.Scale
	}
	silent := filepath.Join(outDir, "silent.mp4")
	meta, err := s.renderer.Video(ctx, dir, "composition.html", opts, silent)
	if err != nil {
		return nil, err
	}
	music, err := s.renderMusic(ctx, dir, outDir, meta.Duration)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(outDir, name)
	if err := s.renderer.Mix(ctx, silent, filepath.Join(dir, "narration", "speech.wav"), music, meta.Duration, out); err != nil {
		return nil, err
	}
	poster := filepath.Join(outDir, storydomain.ArtifactPoster)
	if err := s.renderer.Poster(ctx, out, math.Min(1.5, meta.Duration/3), poster); err != nil {
		return nil, err
	}
	return map[string]string{name: out, storydomain.ArtifactPoster: poster}, nil
}

// FinishRender stores the rendered files on the revision and moves the
// deliverable to review (preview) or ready (final).
func (s *Service) FinishRender(ctx context.Context, task RenderTask, files map[string]string) error {
	p := task.Payload
	st, err := s.stories.GetByID(ctx, p.StoryID)
	if err != nil {
		return err
	}
	d, err := st.Deliverable(p.DeliverableID)
	if err != nil {
		return jobdomain.Permanent(err)
	}
	if d.Revision() != task.Revision {
		return nil // a newer revision replaced this one meanwhile
	}
	prefix := fmt.Sprintf("ws/%s/stories/%s/%s/r%d/", st.WorkspaceID(), st.ID(), d.ID(), d.Revision())
	keys := map[string]string{}
	var length time.Duration
	for artifact, path := range files {
		switch artifact {
		case storydomain.ArtifactPreview, storydomain.ArtifactFinal:
			length = videoLength(path)
		case storydomain.ArtifactPoster:
		default:
			continue
		}
		if err := s.putFile(ctx, prefix+artifact, path); err != nil {
			return err
		}
		keys[artifact] = prefix + artifact
	}
	st, err = s.mutate(ctx, p.StoryID, func(ctx context.Context, st *storydomain.Story) error {
		rev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
		if err != nil {
			return err
		}
		for k, v := range keys {
			rev.Artifacts[k] = v
		}
		if length > 0 {
			rev.Measures.Duration = length
		}
		if err := s.stories.PutRevision(ctx, rev); err != nil {
			return err
		}
		if err := st.Rendered(d.ID(), length, s.now()); err != nil {
			return err
		}
		if d, _ := st.Deliverable(d.ID()); d.Status() == storydomain.StatusReady {
			return s.ready(ctx, st, d)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if d, _ = st.Deliverable(d.ID()); d.Status() == storydomain.StatusReview {
		s.notify(ctx, Event{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), Kind: "review", Title: st.Idea().Title,
			Detail: fmt.Sprintf("video revision %d is ready for review", d.Revision())})
	}
	return nil
}

// ready moves on from a deliverable that just became ready: it queues
// its publication when the workspace has a target, otherwise it releases
// what was waiting on it right away. Call it inside the transaction that
// made d ready, so the deliverable never sits ready with nothing queued.
func (s *Service) ready(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable) error {
	queued, err := s.autoPublish(ctx, st, d)
	if err != nil || queued {
		return err
	}
	return s.release(ctx, st, d.ID())
}

// release queues the deliverables that were waiting on source. Call it
// inside the transaction that saves st.
func (s *Service) release(ctx context.Context, st *storydomain.Story, source uuid.UUID) error {
	queued, err := st.ReleaseDerived(source, s.now())
	if err != nil {
		return err
	}
	for _, q := range queued {
		if err := s.enqueueStep(ctx, st, q); err != nil {
			return err
		}
	}
	return s.autoShorts(ctx, st, source)
}

// autoShorts queues the agent to make a live long video's shorts when it
// asks for some (its pipeline's or request's shorts per video), once.
// A video the limits rule out is left alone.
func (s *Service) autoShorts(ctx context.Context, st *storydomain.Story, source uuid.UUID) error {
	d, err := st.Deliverable(source)
	if err != nil || !d.IsVideo() || d.Video().Shorts() == 0 {
		return err
	}
	if _, err := st.CanCut(source, s.cfg.Shorts); err != nil {
		s.log.InfoContext(ctx, "no shorts cut from the long video", slog.String("story", st.ID().String()), slog.String("reason", apperror.Message(err)))
		return nil
	}
	p := MomentsPayload{StoryID: st.ID(), SourceID: source, Make: d.Video().Shorts()}
	_, err = s.enqueueScan(ctx, st, p, KindMoments+":auto:"+source.String())
	return err
}

// renderSettings are the host profile's render knobs now.
func (s *Service) renderSettings(ctx context.Context) RenderSettings {
	if s.cfg.Render != nil {
		return s.cfg.Render(ctx)
	}
	return RenderSettings{PreviewScale: s.cfg.PreviewScale, FinalScale: s.cfg.FinalScale}
}

// Approve accepts a deliverable in review and queues what follows, all
// in one transaction.
func (s *Service) Approve(ctx context.Context, workspaceID, storyID, deliverableID, actorID uuid.UUID) (*storydomain.Story, error) {
	return s.change(ctx, workspaceID, storyID, func(ctx context.Context, st *storydomain.Story) error {
		queued, err := st.Approve(deliverableID, actorID, id.New, s.now())
		if err != nil {
			return err
		}
		d, _ := st.Deliverable(deliverableID)
		reused, err := s.reusePreview(ctx, st, d)
		if err != nil {
			return err
		}
		if !reused {
			if err := s.enqueueStep(ctx, st, d); err != nil {
				return err
			}
		}
		for _, q := range queued {
			if err := s.enqueueStep(ctx, st, q); err != nil {
				return err
			}
		}
		if d.Status() == storydomain.StatusReady {
			return s.ready(ctx, st, d)
		}
		return nil
	})
}

// reusePreview finishes an approved video with its preview when that
// was rendered at the story's final quality (or the host profile's) or
// better, so approval does not render the same video again, and reports
// whether it did. Call it inside the transaction that approved d.
func (s *Service) reusePreview(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable) (bool, error) {
	production := st.Brief().Production
	scale, final := production.PreviewQuality.Scale(), production.FinalQuality.Scale()
	if final == 0 {
		final = s.renderSettings(ctx).FinalScale
	}
	if d.Status() != storydomain.StatusFinalizing || scale == 0 || scale < final {
		return false, nil
	}
	rev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
	if err != nil {
		return false, err
	}
	preview, ok := rev.Artifacts[storydomain.ArtifactPreview]
	if !ok {
		return false, nil
	}
	rev.Artifacts[storydomain.ArtifactFinal] = preview
	if err := s.stories.PutRevision(ctx, rev); err != nil {
		return false, err
	}
	return true, st.Rendered(d.ID(), 0, s.now())
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

// Retry resumes a failed deliverable and queues the step it resumes at.
func (s *Service) Retry(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID) (*storydomain.Story, error) {
	return s.change(ctx, workspaceID, storyID, func(ctx context.Context, st *storydomain.Story) error {
		resumed, err := st.Retry(deliverableID, s.now())
		if err != nil {
			return err
		}
		d, _ := st.Deliverable(deliverableID)
		if resumed == storydomain.StatusReady {
			return s.enqueuePublish(ctx, st, d) // a failed publication
		}
		return s.enqueueStep(ctx, st, d)
	})
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

// PublishJob runs job, a deliverable.publish job. A retry of the same
// job resumes a publication its earlier attempt left cut short.
func (s *Service) PublishJob(ctx context.Context, jobID uuid.UUID, p DeliverablePayload) error {
	st, err := s.mutate(ctx, p.StoryID, func(_ context.Context, st *storydomain.Story) error {
		return st.StartPublishing(p.DeliverableID, jobID, s.now())
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
	st, err = s.mutate(ctx, p.StoryID, func(ctx context.Context, st *storydomain.Story) error {
		if err := st.Published(p.DeliverableID, publication, s.now()); err != nil {
			return err
		}
		// Derived deliverables can now be drafted around the live lead.
		return s.release(ctx, st, p.DeliverableID)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, Event{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), Kind: "published", Title: st.Idea().Title,
		Detail: fmt.Sprintf("%s published: %s", d.Kind(), publication.URL)})
	return nil
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

// List returns a page of stories, newest first, and the cursor of the
// next page ("" after the last).
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*storydomain.Story, string, error) {
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

// Delete removes a story, its artifacts and working directories. The
// check that no step is working on it and the deletion are one
// transaction, so a worker cannot start a step in between.
func (s *Service) Delete(ctx context.Context, workspaceID, storyID uuid.UUID) error {
	var st *storydomain.Story
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if st, err = s.stories.Get(ctx, workspaceID, storyID); err != nil {
			return err
		}
		if err := st.CheckDeletable(); err != nil {
			return err
		}
		return s.stories.Delete(ctx, workspaceID, storyID)
	})
	if err != nil {
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

// renderMusic renders the video's music to a WAV in outDir: the composed
// music.js, or the library track music.json names. "" when it has none.
func (s *Service) renderMusic(ctx context.Context, dir, outDir string, duration float64) (string, error) {
	out := filepath.Join(outDir, "music.wav")
	if _, err := os.Stat(filepath.Join(dir, "music.js")); err == nil {
		_, err := s.renderer.Music(ctx, dir, duration, out)
		return out, err
	}
	track, ok, err := agenttool.ReadMusicTrack(dir)
	if err != nil || !ok {
		return "", err
	}
	return out, s.renderer.MusicTrack(ctx, filepath.Join(dir, track.File), track.Offset, duration, out)
}
