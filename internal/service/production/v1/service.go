// Package production turns pipeline runs and ad-hoc requests into
// stories, drives each deliverable through the authoring agent and the
// renderer, and applies review decisions.
//
// Job flow:
//
//	pipeline.run / story.adhoc (control) → ideas → stories
//	deliverable.draft  (agent)  → the agent writes a revision in its work dir
//	deliverable.render (render) → preview (review) or final (ready)
//	deliverable.publish (io)    → YouTube / blog
package production

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/claude"
	"kurgu/internal/adapter/render"
	"kurgu/internal/adapter/speech"
	"kurgu/internal/apperror"
	"kurgu/internal/blob"
	"kurgu/internal/db"
	jobdomain "kurgu/internal/domain/job/v1"
	librarydomain "kurgu/internal/domain/library/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	seodomain "kurgu/internal/domain/seo/v1"
	storydomain "kurgu/internal/domain/story/v1"
	voicedomain "kurgu/internal/domain/voice/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	defaultssvc "kurgu/internal/service/defaults/v1"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
	librarysvc "kurgu/internal/service/library/v1"
)

// Job kinds.
const (
	KindAdHoc   = "story.adhoc"
	KindDraft   = "deliverable.draft"
	KindRender  = "deliverable.render"
	KindPublish = "deliverable.publish"
)

// DeliverablePayload addresses one deliverable.
type DeliverablePayload struct {
	StoryID       uuid.UUID `json:"storyId"`
	DeliverableID uuid.UUID `json:"deliverableId"`
	Final         bool      `json:"final,omitempty"`
}

// Agent runs the authoring agent (internal/adapter/claude).
type Agent interface {
	Run(ctx context.Context, req claude.Request) (claude.Result, error)
}

// Renderer renders compositions (internal/adapter/render).
type Renderer interface {
	Probe(ctx context.Context, dir, pagePath string) (render.Meta, []string, error)
	Video(ctx context.Context, dir, pagePath string, opts render.VideoOptions, out string) (render.Meta, error)
	Music(ctx context.Context, dir string, duration float64, out string) (float64, error)
	MusicTrack(ctx context.Context, in string, offset, duration float64, out string) error
	Mix(ctx context.Context, video, narration, music string, duration float64, out string) error
	Poster(ctx context.Context, video string, t float64, out string) error
	Thumbnails(ctx context.Context, dir, pagePath string, ns []int, small bool, outDir string) ([]render.ThumbnailShot, render.Meta, []string, error)
}

// Library is the part of the media library production needs: what a
// draft may use and a record of what it used, and saving what drafts
// made or fetched (internal/service/library).
type Library interface {
	Catalog(ctx context.Context, workspaceID uuid.UUID, kinds []librarydomain.Kind, orientation librarydomain.Orientation) ([]librarysvc.Entry, error)
	RecordUsage(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, revision int, assetIDs []uuid.UUID) error
	Collect(ctx context.Context, workspaceID uuid.UUID) (librarydomain.Collect, error)
	Get(ctx context.Context, workspaceID, assetID uuid.UUID) (*librarydomain.Asset, error)
	Find(ctx context.Context, workspaceID uuid.UUID, h librarysvc.Harvestable) (*librarydomain.Asset, error)
	Save(ctx context.Context, workspaceID, actorID uuid.UUID, h librarysvc.Harvestable, kept bool) (*librarydomain.Asset, error)
	Settle(ctx context.Context, workspaceID, deliverableID uuid.UUID, decided librarysvc.Decided) (int, error)
}

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// Pipelines reads pipelines.
type Pipelines interface {
	Get(ctx context.Context, workspaceID, pipelineID uuid.UUID) (*pipelinedomain.Pipeline, error)
}

// Knowledge reads the knowledge base.
type Knowledge interface {
	Excerpt(ctx context.Context, workspaceID uuid.UUID, refs []knowledgesvc.Ref) (knowledgesvc.Excerpt, error)
	PageTokens(ctx context.Context, workspaceID, documentID uuid.UUID, from, to int) ([]knowledgesvc.PageCost, error)
}

// Opportunities hands out SEO keyword opportunities (the seo service).
type Opportunities interface {
	Next(ctx context.Context, workspaceID uuid.UUID, language string, n int) ([]seodomain.Opportunity, error)
	// Claim gives an open opportunity to a story, reporting false when
	// it is no longer open.
	Claim(ctx context.Context, workspaceID, opportunityID, storyID uuid.UUID) (bool, error)
}

// Templates resolves a template to the style a story keeps (the
// template service).
type Templates interface {
	Style(ctx context.Context, workspaceID, templateID uuid.UUID) (*storydomain.TemplateStyle, error)
}

// Voices resolves narration settings (the voice service).
type Voices interface {
	Options(ctx context.Context, workspaceID uuid.UUID, language string, o *voicedomain.Override) (speech.Options, error)
	CheckOverride(ctx context.Context, language string, o voicedomain.Override) error
}

// Defaults reads a workspace's production defaults and default template
// (the defaults service).
type Defaults interface {
	Get(ctx context.Context, workspaceID uuid.UUID) (defaultssvc.Workspace, error)
}

// Notifier tells people that something needs them (M5: email digests).
type Notifier interface {
	Notify(ctx context.Context, e Event) error
}

// Event is something worth telling a workspace about.
type Event struct {
	WorkspaceID uuid.UUID
	StoryID     uuid.UUID
	Kind        string // review | failed | published | run
	Title       string
	Detail      string
}

// Publisher puts an approved deliverable live (YouTube, the blog).
type Publisher interface {
	// Kind is the deliverable kind it handles (video or article).
	Kind() pipelinedomain.Kind
	// Ready reports whether the workspace has a target configured.
	Ready(ctx context.Context, workspaceID uuid.UUID) (bool, error)
	Publish(ctx context.Context, req PublishRequest) (storydomain.Publication, error)
}

// Dedupe decides whether an idea repeats an earlier story (M5: Jev).
type Dedupe interface {
	Duplicate(ctx context.Context, candidate storydomain.Idea, similar []storyrepo.Match) (bool, string, error)
}

// RenderSettings are the host profile's render knobs, read per render.
type RenderSettings struct {
	Duty         float64
	Chunks       int
	PreviewScale float64
	FinalScale   float64
	PreviewFPS   int
	FinalFPS     int
}

// Config tunes production.
type Config struct {
	WorkRoot     string        // per-deliverable working directories
	KurguBin     string        // absolute path of this binary, for `kurgu tool`
	ToolEnv      []string      // KURGU_* settings for the tools
	PreviewScale float64       // 0.5 = 540p review renders
	FinalScale   float64       // 1.0 = 1080p
	PreviewFPS   int           // frame rate of review renders, default 30
	FinalFPS     int           // frame rate of final renders, default 30
	AgentTimeout time.Duration // per agent call
	FixAttempts  int           // agent retries when its output fails validation
	// Render, when set, overrides the scales per render and supplies the
	// duty cycle and chunks (from the worker's host profile).
	Render func(ctx context.Context) RenderSettings
	// ExtraEnv adds settings read per call (e.g. the Pexels key from the
	// settings page) to the tools' environment.
	ExtraEnv func(ctx context.Context) []string
	// Shorts bound cutting shorts from long videos; the zero value
	// allows none.
	Shorts storydomain.ShortsLimits
	// LibraryEnv lets `kurgu tool library` reach the library's files:
	// the blob store settings, with an absolute directory.
	LibraryEnv []string
}

// Service runs production use cases.
type Service struct {
	cfg        Config
	stories    storyrepo.Repository
	pipelines  Pipelines
	knowledge  Knowledge
	jobs       Enqueuer
	tx         db.TxManager
	blobs      blob.Store
	agent      Agent
	renderer   Renderer
	notifier   Notifier
	dedupe     Dedupe
	seo        Opportunities
	templates  Templates
	voices     Voices
	library    Library
	defaults   Defaults
	publishers map[pipelinedomain.Kind]Publisher
	log        *slog.Logger
	now        func() time.Time
}

// Deps are the collaborators.
type Deps struct {
	Stories   storyrepo.Repository
	Pipelines Pipelines
	Knowledge Knowledge
	Jobs      Enqueuer
	Tx        db.TxManager
	Blobs     blob.Store
	Agent     Agent
	Renderer  Renderer
	Notifier  Notifier
	Dedupe    Dedupe
	SEO       Opportunities
	Templates Templates
	Voices    Voices
	// Library is optional: without it drafts compose and search as ever.
	Library Library
	// Defaults is optional: without it stories take only their pipeline's
	// or request's settings and template over the server's.
	Defaults Defaults
	Log      *slog.Logger
}

// NewService returns a Service.
func NewService(cfg Config, d Deps) *Service {
	if cfg.PreviewScale == 0 {
		cfg.PreviewScale = 0.5
	}
	if cfg.FinalScale == 0 {
		cfg.FinalScale = 1
	}
	if cfg.PreviewFPS == 0 {
		cfg.PreviewFPS = int(pipelinedomain.DefaultFPS)
	}
	if cfg.FinalFPS == 0 {
		cfg.FinalFPS = int(pipelinedomain.DefaultFPS)
	}
	if cfg.AgentTimeout == 0 {
		cfg.AgentTimeout = 45 * time.Minute
	}
	if cfg.FixAttempts == 0 {
		cfg.FixAttempts = 2
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	notifier := d.Notifier
	if notifier == nil {
		notifier = nopNotifier{}
	}
	return &Service{
		cfg: cfg, stories: d.Stories, pipelines: d.Pipelines, knowledge: d.Knowledge, jobs: d.Jobs, tx: d.Tx,
		blobs: d.Blobs, agent: d.Agent, renderer: d.Renderer, notifier: notifier, dedupe: d.Dedupe, seo: d.SEO, templates: d.Templates, voices: d.Voices,
		library: d.Library, defaults: d.Defaults, publishers: map[pipelinedomain.Kind]Publisher{}, log: log, now: time.Now,
	}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// AddPublisher registers a publisher for its deliverable kind.
func (s *Service) AddPublisher(p Publisher) *Service {
	s.publishers[p.Kind()] = p
	return s
}

type nopNotifier struct{}

func (nopNotifier) Notify(context.Context, Event) error { return nil }

func (s *Service) notify(ctx context.Context, e Event) {
	if err := s.notifier.Notify(ctx, e); err != nil {
		s.log.WarnContext(ctx, "notify failed", slog.String("kind", e.Kind), slog.Any("error", err))
	}
}

// enqueueStep queues the job that moves deliverable d forward from its
// current status, if any.
func (s *Service) enqueueStep(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable) error {
	p := DeliverablePayload{StoryID: st.ID(), DeliverableID: d.ID()}
	var (
		queue jobdomain.Queue
		kind  string
	)
	switch d.Status() {
	case storydomain.StatusQueued:
		queue, kind = jobdomain.QueueAgent, KindDraft
	case storydomain.StatusRendering:
		queue, kind = jobdomain.QueueRender, KindRender
	case storydomain.StatusFinalizing:
		p.Final = true
		queue, kind = jobdomain.QueueRender, KindRender
	default:
		return nil
	}
	payload, _ := json.Marshal(p)
	// One attempt: a failed step marks the deliverable failed and a
	// person retries it, rather than spending another agent run blindly.
	_, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{
		WorkspaceID: st.WorkspaceID(), Queue: queue, Kind: kind, Payload: payload, MaxAttempts: 1, TraceID: st.TraceID(),
	})
	return err
}

func (s *Service) enqueuePublish(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable) error {
	payload, _ := json.Marshal(DeliverablePayload{StoryID: st.ID(), DeliverableID: d.ID()})
	_, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{
		WorkspaceID: st.WorkspaceID(), Queue: jobdomain.QueueIO, Kind: KindPublish, Payload: payload, MaxAttempts: 3,
		TraceID: st.TraceID(),
	})
	return err
}

// save writes st, retrying the mutation once on a stale version (a
// reviewer and a worker touching the same story at once).
func (s *Service) mutate(ctx context.Context, storyID uuid.UUID, fn func(ctx context.Context, st *storydomain.Story) error) (*storydomain.Story, error) {
	var out *storydomain.Story
	for attempt := 0; ; attempt++ {
		err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
			st, err := s.stories.GetByID(ctx, storyID)
			if err != nil {
				return err
			}
			if err := fn(ctx, st); err != nil {
				return err
			}
			if err := s.stories.Save(ctx, st); err != nil {
				return err
			}
			out = st
			return nil
		})
		if err == nil || attempt > 0 || !isStale(err) {
			return out, err
		}
	}
}

// style resolves a template id; Nil means no template.
func (s *Service) style(ctx context.Context, workspaceID, templateID uuid.UUID) (*storydomain.TemplateStyle, error) {
	if templateID == uuid.Nil || s.templates == nil {
		return nil, nil
	}
	st, err := s.templates.Style(ctx, workspaceID, templateID)
	if apperror.Is(err, apperror.NotFound) {
		s.log.WarnContext(ctx, "template gone; producing without it", slog.String("template", templateID.String()))
		return nil, nil
	}
	return st, err
}
