// Package app is the composition root: it opens storage, wires
// repositories into services and services into transports.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"ekin/internal/adapter/claude"
	"ekin/internal/adapter/cpulimit"
	"ekin/internal/adapter/elevenlabs"
	"ekin/internal/adapter/extract"
	"ekin/internal/adapter/github"
	"ekin/internal/adapter/media"
	"ekin/internal/adapter/render"
	"ekin/internal/adapter/speech"
	"ekin/internal/adapter/youtube"
	"ekin/internal/blob"
	"ekin/internal/config"
	"ekin/internal/db"
	settingsdomain "ekin/internal/domain/settings/v1"
	identityrepo "ekin/internal/repository/identity/v1"
	jobrepo "ekin/internal/repository/job/v1"
	knowledgerepo "ekin/internal/repository/knowledge/v1"
	libraryrepo "ekin/internal/repository/library/v1"
	notifyrepo "ekin/internal/repository/notify/v1"
	pipelinerepo "ekin/internal/repository/pipeline/v1"
	publishingrepo "ekin/internal/repository/publishing/v1"
	seorepo "ekin/internal/repository/seo/v1"
	settingsrepo "ekin/internal/repository/settings/v1"
	storyrepo "ekin/internal/repository/story/v1"
	templaterepo "ekin/internal/repository/template/v1"
	workspacerepo "ekin/internal/repository/workspace/v1"
	defaultssvc "ekin/internal/service/defaults/v1"
	identitysvc "ekin/internal/service/identity/v1"
	jobsvc "ekin/internal/service/job/v1"
	knowledgesvc "ekin/internal/service/knowledge/v1"
	librarysvc "ekin/internal/service/library/v1"
	notifysvc "ekin/internal/service/notify/v1"
	pipelinesvc "ekin/internal/service/pipeline/v1"
	productionsvc "ekin/internal/service/production/v1"
	publishingsvc "ekin/internal/service/publishing/v1"
	seosvc "ekin/internal/service/seo/v1"
	settingssvc "ekin/internal/service/settings/v1"
	templatesvc "ekin/internal/service/template/v1"
	voicesvc "ekin/internal/service/voice/v1"
	workspacesvc "ekin/internal/service/workspace/v1"
	httptransport "ekin/internal/transport/http"
	systemhandler "ekin/internal/transport/http/system/v1"
	knowledgeworker "ekin/internal/transport/job/knowledge/v1"
	libraryworker "ekin/internal/transport/job/library/v1"
	productionworker "ekin/internal/transport/job/production/v1"
	seoworker "ekin/internal/transport/job/seo/v1"
	templateworker "ekin/internal/transport/job/template/v1"
	voiceworker "ekin/internal/transport/job/voice/v1"
	"ekin/internal/transport/schedule"
	"ekin/pkg/password"
	"ekin/pkg/secretbox"
	"ekin/web"
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
	library    *librarysvc.Service
	production *productionsvc.Service
	settings   *settingssvc.Service
	notify     *notifysvc.Service
	publishing *publishingsvc.Service
	seo        *seosvc.Service
	templates  *templatesvc.Service
	voices     *voicesvc.Service
	defaults   *defaultssvc.Service
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
	library := librarysvc.NewService(libraryrepo.NewSQLite(tx), blobs, jobs,
		&media.Processor{FFmpeg: cfg.Production.FFmpeg, Threads: cfg.Production.Threads}, tx)
	identity := identitysvc.NewService(identities, workspaces, tx, password.New(password.Default))
	workspaceSvc := workspacesvc.NewService(workspaces, identities, tx)
	box, err := openSecretBox(cfg.Publishing)
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	settings := settingssvc.NewService(settingsrepo.NewSQLite(tx), tx, defaultProfile(cfg), log.With(slog.String("component", "settings"))).
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
	defaults := defaultssvc.NewService(settingsrepo.NewSQLite(tx), settings,
		defaultssvc.Config{Profile: cfg.Worker.Profile, Effort: cfg.Production.ClaudeEffort}).WithTemplates(templates)
	production, err := newProduction(cfg, settings, hostProfile, productionsvc.Deps{
		Defaults:  defaults,
		SEO:       seo,
		Templates: templates,
		Voices:    voices,
		Stories:   storyrepo.NewSQLite(tx), Pipelines: pipelines, Knowledge: knowledge, Jobs: jobs, Tx: tx, Blobs: blobs,
		Notifier: notify,
		Library:  library,
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
		library:    library,
		production: production,
		settings:   settings,
		notify:     notify,
		publishing: publishing,
		seo:        seo,
		templates:  templates,
		voices:     voices,
		defaults:   defaults,
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

// defaultProfile is the host profile built from EKIN_* variables; it
// applies until an admin saves the profile in the UI.
func defaultProfile(cfg config.Config) settingsdomain.Profile {
	return settingsdomain.Profile{
		Name: cfg.Worker.Profile, Concurrency: cfg.Worker.Concurrency, HeavyExclusive: cfg.Worker.HeavyExclusive,
		Cooldown: cfg.Worker.Cooldown, Paused: cfg.Worker.Paused, CPULimit: cfg.Worker.CPULimit,
		HeavyQueues: cfg.Worker.HeavyQueues, Nice: cfg.Worker.Nice,
		RenderDuty: cfg.Production.Duty, RenderChunks: cfg.Production.Chunks,
		PreviewScale: cfg.Production.PreviewScale, FinalScale: cfg.Production.FinalScale,
		PreviewFPS: cfg.Production.PreviewFPS, FinalFPS: cfg.Production.FinalFPS,
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
		"EKIN_SPEECH_DIR=" + p.SpeechDir, "EKIN_UV=" + p.UV,
		"EKIN_CHROME=" + p.Chrome, "EKIN_FFMPEG=" + p.FFmpeg, "EKIN_SPEECH_MODELS=" + p.SpeechModels,
	}
	if err := os.MkdirAll(p.WorkDir, 0o750); err != nil {
		return nil, err
	}
	libraryEnv, err := libraryEnv(cfg.Blob)
	if err != nil {
		return nil, err
	}
	pc := productionsvc.Config{
		LibraryEnv: libraryEnv,
		WorkRoot: p.WorkDir, EkinBin: bin, ToolEnv: toolEnv, AgentTimeout: p.AgentTimeout,
		PreviewScale: p.PreviewScale, FinalScale: p.FinalScale, PreviewFPS: p.PreviewFPS, FinalFPS: p.FinalFPS,
		Shorts: cfg.Shorts,
	}
	pc.ExtraEnv = func(context.Context) []string {
		return []string{"EKIN_PEXELS_API_KEY=" + p.PexelsKey, "EKIN_ELEVENLABS_API_KEY=" + p.ElevenLabsKey}
	}
	if settings != nil {
		pc.ExtraEnv = func(ctx context.Context) []string {
			c := settings.Credentials(ctx)
			return []string{"EKIN_PEXELS_API_KEY=" + c.PexelsAPIKey, "EKIN_ELEVENLABS_API_KEY=" + c.ElevenLabsAPIKey}
		}
	}
	if hostProfile != nil {
		pc.Render = func(ctx context.Context) productionsvc.RenderSettings {
			hp := hostProfile(ctx)
			return productionsvc.RenderSettings{Duty: hp.RenderDuty, Chunks: hp.RenderChunks, PreviewScale: hp.PreviewScale, FinalScale: hp.FinalScale,
				PreviewFPS: hp.PreviewRate(), FinalFPS: hp.FinalRate()}
		}
	} else {
		pc.Render = func(context.Context) productionsvc.RenderSettings {
			return productionsvc.RenderSettings{Duty: p.Duty, Chunks: p.Chunks, PreviewScale: p.PreviewScale, FinalScale: p.FinalScale,
				PreviewFPS: p.PreviewFPS, FinalFPS: p.FinalFPS}
		}
	}
	return productionsvc.NewService(pc, deps), nil
}

// libraryEnv is the blob store's settings for `ekin tool library`, which
// runs in a draft's working directory: a relative blob directory is made
// absolute.
func libraryEnv(c config.Blob) ([]string, error) {
	if c.Driver == "s3" {
		return []string{"EKIN_BLOB_DRIVER=s3", "EKIN_S3_ENDPOINT=" + c.S3.Endpoint, "EKIN_S3_REGION=" + c.S3.Region,
			"EKIN_S3_BUCKET=" + c.S3.Bucket, "EKIN_S3_PREFIX=" + c.S3.Prefix, "EKIN_S3_ACCESS_KEY_ID=" + c.S3.AccessKey,
			"EKIN_S3_SECRET_ACCESS_KEY=" + c.S3.SecretKey}, nil
	}
	dir, err := filepath.Abs(c.Dir)
	if err != nil {
		return nil, err
	}
	return []string{"EKIN_BLOB_DRIVER=fs", "EKIN_BLOB_DIR=" + dir}, nil
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
		Library:     a.library,
		Production:  a.production,
		Feed:        a.notify,
		Inviter:     inviter{a.notify, a.workspaces},
		Publishing:  a.publishing,
		SEO:         a.seo,
		Templates:   a.templates,
		Voices:      a.voices,
		Defaults:    a.defaults,
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
	libraryworker.Register(runner, a.library)
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
