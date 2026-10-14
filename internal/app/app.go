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
	"kurgu/internal/adapter/render"
	"kurgu/internal/blob"
	"kurgu/internal/config"
	"kurgu/internal/db"
	identityrepo "kurgu/internal/repository/identity/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	knowledgerepo "kurgu/internal/repository/knowledge/v1"
	pipelinerepo "kurgu/internal/repository/pipeline/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	identitysvc "kurgu/internal/service/identity/v1"
	jobsvc "kurgu/internal/service/job/v1"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	productionsvc "kurgu/internal/service/production/v1"
	workspacesvc "kurgu/internal/service/workspace/v1"
	httptransport "kurgu/internal/transport/http"
	knowledgeworker "kurgu/internal/transport/job/knowledge/v1"
	productionworker "kurgu/internal/transport/job/production/v1"
	"kurgu/internal/transport/schedule"
	"kurgu/pkg/password"
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
	production, err := newProduction(cfg, log, productionsvc.Deps{
		Stories: storyrepo.NewSQLite(tx), Pipelines: pipelines, Knowledge: knowledge, Jobs: jobs, Tx: tx, Blobs: blobs,
		Log: log.With(slog.String("component", "production")),
	})
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	return &App{
		cfg:        cfg,
		log:        log,
		db:         d,
		identity:   identitysvc.NewService(identities, workspaces, tx, password.New(password.Default)),
		workspaces: workspacesvc.NewService(workspaces, identities, tx),
		pipelines:  pipelines,
		jobs:       jobs,
		knowledge:  knowledge,
		production: production,
	}, nil
}

func newProduction(cfg config.Config, log *slog.Logger, deps productionsvc.Deps) (*productionsvc.Service, error) {
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
	_ = log
	return productionsvc.NewService(productionsvc.Config{
		WorkRoot: p.WorkDir, KurguBin: bin, ToolEnv: toolEnv, AgentTimeout: p.AgentTimeout,
		PreviewScale: p.PreviewScale, FinalScale: p.FinalScale,
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
		Owner: w.Owner,
		Throttle: jobsvc.Throttle{
			Concurrency:    w.Concurrency,
			HeavyExclusive: w.HeavyExclusive,
			Cooldown:       w.Cooldown,
			Paused:         w.Paused,
		},
		Lease: w.Lease,
	}, a.log.With(slog.String("component", "worker")))
	knowledgeworker.Register(runner, a.knowledge)
	productionworker.Register(runner, a.production)
	a.log.InfoContext(ctx, "worker started", slog.String("owner", w.Owner), slog.Any("concurrency", w.Concurrency))
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
