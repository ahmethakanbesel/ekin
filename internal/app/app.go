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
	"kurgu/internal/adapter/extract"
	"kurgu/internal/adapter/github"
	"kurgu/internal/adapter/openrouter"
	"kurgu/internal/adapter/render"
	"kurgu/internal/adapter/smtp"
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
	settingsrepo "kurgu/internal/repository/settings/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	identitysvc "kurgu/internal/service/identity/v1"
	jobsvc "kurgu/internal/service/job/v1"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
	notifysvc "kurgu/internal/service/notify/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	productionsvc "kurgu/internal/service/production/v1"
	publishingsvc "kurgu/internal/service/publishing/v1"
	settingssvc "kurgu/internal/service/settings/v1"
	workspacesvc "kurgu/internal/service/workspace/v1"
	httptransport "kurgu/internal/transport/http"
	systemhandler "kurgu/internal/transport/http/system/v1"
	knowledgeworker "kurgu/internal/transport/job/knowledge/v1"
	productionworker "kurgu/internal/transport/job/production/v1"
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
	knowledge := knowledgesvc.NewService(knowledgerepo.NewSQLite(tx), blobs, jobs, &extract.Extractor{PDFToText: cfg.Tools.PDFToText}, tx)
	pipelines := pipelinesvc.NewService(pipelinerepo.NewSQLite(tx), workspaces, jobs, tx).WithDocuments(knowledge)
	knowledge.WithUsage(pipelines)
	identity := identitysvc.NewService(identities, workspaces, tx, password.New(password.Default))
	workspaceSvc := workspacesvc.NewService(workspaces, identities, tx)
	settings := settingssvc.NewService(settingsrepo.NewSQLite(tx), defaultProfile(cfg), log.With(slog.String("component", "settings")))
	mailer := smtp.New(smtp.Config{
		Host: cfg.Notify.SMTPHost, Port: cfg.Notify.SMTPPort, Username: cfg.Notify.SMTPUser,
		Password: cfg.Notify.SMTPPassword, From: cfg.Notify.SMTPFrom, TLS: cfg.Notify.SMTPTLS,
	})
	notify := notifysvc.NewService(notifyrepo.NewSQLite(tx), directory{identity, workspaceSvc}, mailer, cfg.PublicURL,
		log.With(slog.String("component", "notify")))
	box, err := openSecretBox(cfg.Publishing)
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	publishing := publishingsvc.NewService(publishingrepo.NewSQLite(tx), box,
		&youtube.Client{ClientID: cfg.Publishing.YouTubeClientID, ClientSecret: cfg.Publishing.YouTubeClientSecret,
			RedirectURL: cfg.PublicURL + "/api/v1/oauth/youtube/callback"},
		&github.Client{}, log.With(slog.String("component", "publishing")))
	production, err := newProduction(cfg, settings, productionsvc.Deps{
		Stories: storyrepo.NewSQLite(tx), Pipelines: pipelines, Knowledge: knowledge, Jobs: jobs, Tx: tx, Blobs: blobs,
		Notifier: notify,
		Dedupe:   openrouter.Dedupe{Client: &openrouter.Client{APIKey: cfg.Notify.OpenRouterKey, Model: cfg.Notify.DecisionModel}},
		Log:      log.With(slog.String("component", "production")),
	})
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	production.AddPublisher(publishing.Video()).AddPublisher(publishing.Article())
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
		Cooldown: cfg.Worker.Cooldown, Paused: cfg.Worker.Paused,
		RenderDuty: cfg.Production.Duty, RenderChunks: cfg.Production.Chunks,
		PreviewScale: cfg.Production.PreviewScale, FinalScale: cfg.Production.FinalScale,
	}
}

func newProduction(cfg config.Config, settings *settingssvc.Service, deps productionsvc.Deps) (*productionsvc.Service, error) {
	p := cfg.Production
	bin, err := os.Executable()
	if err != nil {
		return nil, err
	}
	renderer := &render.Renderer{
		Chrome: p.Chrome, FFmpeg: p.FFmpeg, Encoder: p.Encoder, Chunks: p.Chunks, Duty: p.Duty,
		Threads: p.Threads, GPU: p.GPU,
	}
	deps.Agent = &claude.Runner{Bin: p.ClaudeBin, Model: p.ClaudeModel}
	deps.Renderer = renderer
	toolEnv := []string{
		"KURGU_SPEECH_DIR=" + p.SpeechDir, "KURGU_UV=" + p.UV, "KURGU_PEXELS_API_KEY=" + p.PexelsKey,
		"KURGU_CHROME=" + p.Chrome, "KURGU_FFMPEG=" + p.FFmpeg, "KURGU_SPEECH_MODELS=" + p.SpeechModels,
	}
	if err := os.MkdirAll(p.WorkDir, 0o750); err != nil {
		return nil, err
	}
	profile := cfg.Worker.Profile
	return productionsvc.NewService(productionsvc.Config{
		WorkRoot: p.WorkDir, KurguBin: bin, ToolEnv: toolEnv, AgentTimeout: p.AgentTimeout,
		PreviewScale: p.PreviewScale, FinalScale: p.FinalScale,
		Render: func(ctx context.Context) productionsvc.RenderSettings {
			hp := settings.Cached(ctx, profile)
			return productionsvc.RenderSettings{Duty: hp.RenderDuty, Chunks: hp.RenderChunks, PreviewScale: hp.PreviewScale, FinalScale: hp.FinalScale}
		},
	}, deps), nil
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

// Serve runs the HTTP API, the web UI and the scheduler until ctx ends.
func (a *App) Serve(ctx context.Context) error {
	router, err := httptransport.NewRouter(httptransport.Deps{
		Identity:   a.identity,
		Workspaces: a.workspaces,
		Pipelines:  a.pipelines,
		Jobs:       a.jobs,
		Knowledge:  a.knowledge,
		Production: a.production,
		Feed:       a.notify,
		Inviter:    inviter{a.notify, a.workspaces},
		Publishing: a.publishing,
		System:     systemhandler.New(a.settings, a.cfg.Worker.Profile, a.version, a.checks),
		PublicURL:  a.cfg.PublicURL,
		UI:         web.Dist(),
		Log:        a.log,
	})
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
		Owner:          w.Owner,
		ThrottleSource: a.settings.ThrottleSource(w.Profile),
		Lease:          w.Lease,
	}, a.log.With(slog.String("component", "worker")))
	knowledgeworker.Register(runner, a.knowledge)
	productionworker.Register(runner, a.production)
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
