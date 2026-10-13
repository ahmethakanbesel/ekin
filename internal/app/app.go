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

	"kurgu/internal/config"
	"kurgu/internal/db"
	jobrepo "kurgu/internal/repository/job/v1"
	pipelinerepo "kurgu/internal/repository/pipeline/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	jobsvc "kurgu/internal/service/job/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	workspacesvc "kurgu/internal/service/workspace/v1"
	httptransport "kurgu/internal/transport/http"
	"kurgu/internal/transport/schedule"
	"kurgu/web"
)

// App holds the wired process.
type App struct {
	cfg        config.Config
	log        *slog.Logger
	db         *db.DB
	workspaces *workspacesvc.Service
	pipelines  *pipelinesvc.Service
	jobs       *jobsvc.Service
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
	tx := db.NewTxManager(d)
	workspaces := workspacerepo.NewSQLite(tx)
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	return &App{
		cfg:        cfg,
		log:        log,
		db:         d,
		workspaces: workspacesvc.NewService(workspaces),
		pipelines:  pipelinesvc.NewService(pipelinerepo.NewSQLite(tx), workspaces, jobs, tx),
		jobs:       jobs,
	}, nil
}

// Close releases the database.
func (a *App) Close() error { return a.db.Close() }

// Serve runs the HTTP API, the web UI and the scheduler until ctx ends.
func (a *App) Serve(ctx context.Context) error {
	srv := &http.Server{
		Addr: a.cfg.HTTPAddr,
		Handler: httptransport.NewRouter(httptransport.Deps{
			Workspaces: a.workspaces,
			Pipelines:  a.pipelines,
			Jobs:       a.jobs,
			UI:         web.Dist(),
			Log:        a.log,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go schedule.Run(ctx, a.pipelines, a.cfg.SchedulerTick, a.log.With(slog.String("component", "scheduler")))

	errc := make(chan error, 1)
	go func() {
		a.log.InfoContext(ctx, "http listening", slog.String("addr", a.cfg.HTTPAddr))
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
	// Job handlers register here as their contexts land (pipeline.run,
	// story.*, render.*). Until then the runner leaves jobs queued.
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
