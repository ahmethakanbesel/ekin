// Package app is the composition root: it opens storage, wires
// repositories into services and services into transports.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"kurgu/internal/adapter/claude"
	"kurgu/internal/adapter/cpulimit"
	"kurgu/internal/adapter/elevenlabs"
	"kurgu/internal/adapter/extract"
	"kurgu/internal/adapter/github"
	"kurgu/internal/adapter/render"
	"kurgu/internal/adapter/speech"
	"kurgu/internal/adapter/youtube"
	"kurgu/internal/blob"
	"kurgu/internal/config"
	"kurgu/internal/db"
	settingsdomain "kurgu/internal/domain/settings/v1"
	identityrepo "kurgu/internal/repository/identity/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	knowledgerepo "kurgu/internal/repository/knowledge/v1"
	notifyrepo "kurgu/internal/repository/notify/v1"
	pipelinerepo "kurgu/internal/repository/pipeline/v1"
	publishingrepo "kurgu/internal/repository/publishing/v1"
	seorepo "kurgu/internal/repository/seo/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	templaterepo "kurgu/internal/repository/template/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	identitysvc "kurgu/internal/service/identity/v1"
	jobsvc "kurgu/internal/service/job/v1"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
	notifysvc "kurgu/internal/service/notify/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	productionsvc "kurgu/internal/service/production/v1"
	publishingsvc "kurgu/internal/service/publishing/v1"
	seosvc "kurgu/internal/service/seo/v1"
	settingssvc "kurgu/internal/service/settings/v1"
	templatesvc "kurgu/internal/service/template/v1"
	voicesvc "kurgu/internal/service/voice/v1"
	workspacesvc "kurgu/internal/service/workspace/v1"
	httptransport "kurgu/internal/transport/http"
	systemhandler "kurgu/internal/transport/http/system/v1"
	knowledgeworker "kurgu/internal/transport/job/knowledge/v1"
	productionworker "kurgu/internal/transport/job/production/v1"
	seoworker "kurgu/internal/transport/job/seo/v1"
	templateworker "kurgu/internal/transport/job/template/v1"
	voiceworker "kurgu/internal/transport/job/voice/v1"
	"kurgu/internal/transport/schedule"
	"kurgu/pkg/password"
	"kurgu/pkg/secretbox"
	"kurgu/web"
)

// sweepEvery is how often expired sessions and invitations are deleted.
const sweepEvery = time.Hour

// App holds the wired process.
type App struct {
	cfg        config.Config
	log        *slog.Logger
	db         *db.DB
	identity   *identitysvc.Service
	workspaces *workspacesvc.Service
	pipelines  *pipelinesvc.Service
	jobs       *jobsvc.Service
	knowledge  *knowledgesvc.Service
	production *productionsvc.Service
	settings   *settingssvc.Service
	notify     *notifysvc.Service
	publishing *publishingsvc.Service
	seo        *seosvc.Service
	templates  *templatesvc.Service
	voices     *voicesvc.Service
	version    string
}

// New opens the database (migrating it when configured) and wires the
// services.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if cfg.MigrateOnStart {
		if err := db.MigrateUp(ctx, d); err != nil {
			_ = d.Close()
			return nil, err
		}
	}
	blobs, err := openBlobs(cfg.Blob)
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	tx := db.NewTxManager(d)
	identities := identityrepo.NewSQLite(tx)
	workspaces := workspacerepo.NewSQLite(tx)
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	knowledge := knowledgesvc.NewService(knowledgerepo.NewSQLite(tx), blobs, jobs, &extract.Extractor{PDFToText: cfg.Tools.PDFToText, PDFToHTML: cfg.Tools.PDFToHTML}, tx)
	pipelines := pipelinesvc.NewService(pipelinerepo.NewSQLite(tx), workspaces, jobs, tx).WithDocuments(knowledge)
	knowledge.WithUsage(pipelines)
	identity := identitysvc.NewService(identities, workspaces, tx, password.New(password.Default))
	workspaceSvc := workspacesvc.NewService(workspaces, identities, tx)
	box, err := openSecretBox(cfg.Publishing)
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	settings := settingssvc.NewService(settingsrepo.NewSQLite(tx), defaultProfile(cfg), log.With(slog.String("component", "settings"))).
		WithCredentials(envCredentials(cfg), box).WithCredentialDefaults(credentialDefaults())
	notify := notifysvc.NewService(notifyrepo.NewSQLite(tx), directory{identity, workspaceSvc}, dynamicMailer{settings}, cfg.PublicURL,
		log.With(slog.String("component", "notify")))
	publishing := publishingsvc.NewService(publishingrepo.NewSQLite(tx), box,
		func(ctx context.Context) *youtube.Client {
			c := settings.Credentials(ctx)
			return &youtube.Client{ClientID: c.YouTubeClientID, ClientSecret: c.YouTubeClientSecret,
				RedirectURL: cfg.PublicURL + "/api/v1/oauth/youtube/callback"}
		},
		&github.Client{}, log.With(slog.String("component", "publishing")))
	agent := dynamicAgent{settings: settings, base: claude.Runner{Bin: cfg.Production.ClaudeBin, Effort: cfg.Production.ClaudeEffort}}
	seo := seosvc.NewService(seorepo.NewSQLite(tx), settingsrepo.NewSQLite(tx), jobs, agent, cfg.Production.WorkDir).
		WithResearch(openSEO(settings))
	templates := templatesvc.NewService(templaterepo.NewSQLite(tx), jobs, agent, cfg.Production.WorkDir)
	p := cfg.Production
	elevenLabsKey := func(ctx context.Context) string { return settings.Credentials(ctx).ElevenLabsAPIKey }
	voices := voicesvc.NewService(settingsrepo.NewSQLite(tx), jobs,
		&speech.Synth{UV: p.UV, ScriptDir: p.SpeechDir, ModelDir: p.SpeechModels, ElevenLabsKey: elevenLabsKey}, blobs).
		WithElevenLabs(func(ctx context.Context) *elevenlabs.Client { return &elevenlabs.Client{APIKey: elevenLabsKey(ctx)} })
	pipelines.WithTemplates(templates).WithVoices(voices)
	hostProfile := func(ctx context.Context) settingsdomain.Profile { return settings.Cached(ctx, cfg.Worker.Profile) }
	production, err := newProduction(cfg, settings, hostProfile, productionsvc.Deps{
		SEO:       seo,
		Templates: templates,
		Voices:    voices,
		Stories:   storyrepo.NewSQLite(tx), Pipelines: pipelines, Knowledge: knowledge, Jobs: jobs, Tx: tx, Blobs: blobs,
		Notifier: notify,
		Agent:    agent,
		Dedupe:   dynamicDedupe{settings},
		Log:      log.With(slog.String("component", "production")),
	})
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	production.AddPublisher(publishing.Video()).AddPublisher(publishing.Article())
	productionworker.Watch(jobs, production)
	return &App{
		cfg:        cfg,
		log:        log,
		db:         d,
		identity:   identity,
		workspaces: workspaceSvc,
		pipelines:  pipelines,
		jobs:       jobs,
		knowledge:  knowledge,
		production: production,
		settings:   settings,
		notify:     notify,
		publishing: publishing,
		seo:        seo,
		templates:  templates,
		voices:     voices,
	}, nil
}

func openSecretBox(c config.Publishing) (*secretbox.Box, error) {
	if c.SecretKey != "" {
		return secretbox.New([]byte(c.SecretKey))
	}
	return secretbox.LoadOrCreate(c.SecretKeyFile)
}

// WithVersion records the build version for the system endpoint.
func (a *App) WithVersion(v string) *App {
	a.version = v
	return a
}

// defaultProfile is the host profile built from KURGU_* variables; it
// applies until an admin saves the profile in the UI.
func defaultProfile(cfg config.Config) settingsdomain.Profile {
	return settingsdomain.Profile{
		Name: cfg.Worker.Profile, Concurrency: cfg.Worker.Concurrency, HeavyExclusive: cfg.Worker.HeavyExclusive,
		Cooldown: cfg.Worker.Cooldown, Paused: cfg.Worker.Paused, CPULimit: cfg.Worker.CPULimit,
		HeavyQueues: cfg.Worker.HeavyQueues, Nice: cfg.Worker.Nice,
		RenderDuty: cfg.Production.Duty, RenderChunks: cfg.Production.Chunks,
		PreviewScale: cfg.Production.PreviewScale, FinalScale: cfg.Production.FinalScale,
	}
}

// newProduction builds the production service. settings supplies the
// credentials and hostProfile the render settings; either may be nil,
// leaving the environment's values.
func newProduction(cfg config.Config, settings *settingssvc.Service, hostProfile func(context.Context) settingsdomain.Profile,
	deps productionsvc.Deps) (*productionsvc.Service, error) {
	p := cfg.Production
	bin, err := os.Executable()
	if err != nil {
		return nil, err
	}
	renderer := &render.Renderer{
		Chrome: p.Chrome, FFmpeg: p.FFmpeg, Encoder: p.Encoder, Chunks: p.Chunks, Duty: p.Duty,
		Threads: p.Threads, GPU: p.GPU,
	}
	if deps.Agent == nil {
		deps.Agent = &claude.Runner{Bin: p.ClaudeBin, Model: p.ClaudeModel, Effort: p.ClaudeEffort}
	}
	deps.Renderer = renderer
	toolEnv := []string{
		"KURGU_SPEECH_DIR=" + p.SpeechDir, "KURGU_UV=" + p.UV,
		"KURGU_CHROME=" + p.Chrome, "KURGU_FFMPEG=" + p.FFmpeg, "KURGU_SPEECH_MODELS=" + p.SpeechModels,
	}
	if err := os.MkdirAll(p.WorkDir, 0o750); err != nil {
		return nil, err
	}
	pc := productionsvc.Config{
		WorkRoot: p.WorkDir, KurguBin: bin, ToolEnv: toolEnv, AgentTimeout: p.AgentTimeout,
		PreviewScale: p.PreviewScale, FinalScale: p.FinalScale,
	}
	pc.ExtraEnv = func(context.Context) []string {
		return []string{"KURGU_PEXELS_API_KEY=" + p.PexelsKey, "KURGU_ELEVENLABS_API_KEY=" + p.ElevenLabsKey}
	}
	if settings != nil {
		pc.ExtraEnv = func(ctx context.Context) []string {
			c := settings.Credentials(ctx)
			return []string{"KURGU_PEXELS_API_KEY=" + c.PexelsAPIKey, "KURGU_ELEVENLABS_API_KEY=" + c.ElevenLabsAPIKey}
		}
	}
	if hostProfile != nil {
		pc.Render = func(ctx context.Context) productionsvc.RenderSettings {
			hp := hostProfile(ctx)
			return productionsvc.RenderSettings{Duty: hp.RenderDuty, Chunks: hp.RenderChunks, PreviewScale: hp.PreviewScale, FinalScale: hp.FinalScale}
		}
	} else {
		pc.Render = func(context.Context) productionsvc.RenderSettings {
			return productionsvc.RenderSettings{Duty: p.Duty, Chunks: p.Chunks, PreviewScale: p.PreviewScale, FinalScale: p.FinalScale}
		}
	}
	return productionsvc.NewService(pc, deps), nil
}

func openBlobs(c config.Blob) (blob.Store, error) {
	if c.Driver == "s3" {
		return blob.NewS3(blob.S3Config{
			Endpoint: c.S3.Endpoint, Region: c.S3.Region, Bucket: c.S3.Bucket, Prefix: c.S3.Prefix,
			AccessKey: c.S3.AccessKey, SecretKey: c.S3.SecretKey,
		})
	}
	return blob.NewFS(c.Dir)
}

// Close releases the database.
func (a *App) Close() error { return a.db.Close() }

// Identity exposes the identity service to the CLI.
func (a *App) Identity() *identitysvc.Service { return a.identity }

// Workspaces exposes the workspace service to the CLI.
func (a *App) Workspaces() *workspacesvc.Service { return a.workspaces }

// Router builds the HTTP handler with every service wired in.
func (a *App) Router() (http.Handler, error) {
	return httptransport.NewRouter(httptransport.Deps{
		Identity:    a.identity,
		Workspaces:  a.workspaces,
		Pipelines:   a.pipelines,
		Jobs:        a.jobs,
		Knowledge:   a.knowledge,
		Production:  a.production,
		Feed:        a.notify,
		Inviter:     inviter{a.notify, a.workspaces},
		Publishing:  a.publishing,
		SEO:         a.seo,
		Templates:   a.templates,
		Voices:      a.voices,
		WorkerToken: a.cfg.Remote.Token,
		Profiles:    a.settings,
		System:      systemhandler.New(a.settings, a.cfg.Worker.Profile, a.version, a.checks).OnCredentialsChange(resetChecks),
		PublicURL:   a.cfg.PublicURL,
		UI:          web.Dist(),
		Log:         a.log,
	})
}

// Serve runs the HTTP API, the web UI and the scheduler until ctx ends.
func (a *App) Serve(ctx context.Context) error {
	router, err := a.Router()
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              a.cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go schedule.Run(ctx, a.pipelines, a.cfg.SchedulerTick, a.log.With(slog.String("component", "scheduler")))
	go a.sweep(ctx)
	go a.flushNotifications(ctx)

	errc := make(chan error, 1)
	go func() {
		a.log.InfoContext(ctx, "http listening", slog.String("addr", a.cfg.HTTPAddr), slog.String("public_url", a.cfg.PublicURL))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// sweep deletes expired sessions and invitations every sweepEvery.
func (a *App) sweep(ctx context.Context) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		sessions, invitations, err := a.identity.Sweep(ctx)
		if err != nil && ctx.Err() == nil {
			a.log.ErrorContext(ctx, "identity sweep failed", slog.Any("error", err))
		} else if sessions+invitations > 0 {
			a.log.InfoContext(ctx, "identity sweep", slog.Int("sessions", sessions), slog.Int("invitations", invitations))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Work runs the job runner until ctx ends.
func (a *App) Work(ctx context.Context) error {
	w := a.cfg.Worker
	runner := jobsvc.NewRunner(a.jobs, jobsvc.RunnerConfig{
		Owner:           w.Owner,
		ThrottleSource:  a.settings.ThrottleSource(w.Profile),
		ThrottleChanged: a.settings.Changed,
		Lease:           w.Lease,
	}, a.log.With(slog.String("component", "worker")))
	knowledgeworker.Register(runner, a.knowledge)
	productionworker.Register(runner, a.production)
	seoworker.Register(runner, a.seo)
	templateworker.Register(runner, a.templates)
	voiceworker.Register(runner, a.voices)
	limiter := &cpulimit.Limiter{
		Percent: func(ctx context.Context) int { return a.settings.Cached(ctx, w.Profile).CPULimit },
		Changed: a.settings.Changed, Log: a.log.With(slog.String("component", "cpulimit")),
	}
	go limiter.Run(ctx)
	nicer := &cpulimit.Nicer{
		Nice:    func(ctx context.Context) int { return a.settings.Cached(ctx, w.Profile).Nice },
		Changed: a.settings.Changed, Log: a.log.With(slog.String("component", "nice")),
	}
	go nicer.Run(ctx)
	a.log.InfoContext(ctx, "worker started", slog.String("owner", w.Owner), slog.String("profile", w.Profile))
	return runner.Run(ctx)
}

// NewLogger builds the process logger from cfg.
func NewLogger(cfg config.Config) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}
