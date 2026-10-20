// Command ekin runs the Ekin API, scheduler and job worker.
//
//	ekin serve            API + web UI + scheduler
//	ekin worker           job runner
//	ekin all              serve and worker in one process (laptop mode)
//	ekin migrate up|down|status
//	ekin user create|reset-password
//	ekin tool tts|frame|check|stock|music
//	ekin version
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/ahmethakanbesel/ekin/internal/app"
	"github.com/ahmethakanbesel/ekin/internal/config"
	"github.com/ahmethakanbesel/ekin/internal/db"
)

var (
	version = "dev"
	commit  = "none"
)

const usage = `usage: ekin <command>

commands:
  serve                 run the API, web UI and scheduler
  worker                run the job worker (remote with EKIN_REMOTE_URL)
  all                   run serve and worker in one process
  migrate up|down|status
  user create|reset-password
                        manage accounts (see ekin user)
  tool <command>        agent tools: tts, frame, check, stock, music
  version
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ekin:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("missing command")
	}
	if args[0] == "version" {
		fmt.Printf("ekin %s (%s)\n", version, commit)
		return nil
	}
	if args[0] == "tool" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return tool(ctx, args[1:])
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := app.NewLogger(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if args[0] == "worker" && cfg.Remote.URL != "" {
		return app.WorkRemote(ctx, cfg, log)
	}
	if args[0] == "migrate" {
		return migrate(ctx, cfg, args[1:])
	}

	a, err := app.New(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer func() { _ = a.Close() }()
	a.WithVersion(version + " (" + commit + ")")

	switch args[0] {
	case "user":
		return user(ctx, a, args[1:])
	case "serve":
		return a.Serve(ctx)
	case "worker":
		return a.Work(ctx)
	case "all":
		g, ctx := errgroup.WithContext(ctx)
		g.Go(func() error { return a.Serve(ctx) })
		g.Go(func() error { return a.Work(ctx) })
		return g.Wait()
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command %q", args[0])
}

func migrate(ctx context.Context, cfg config.Config, args []string) error {
	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	direction := "up"
	if len(args) > 0 {
		direction = args[0]
	}
	switch direction {
	case "up":
		return db.MigrateUp(ctx, d)
	case "down":
		return db.MigrateDown(ctx, d)
	case "status":
		lines, err := db.MigrationStatus(ctx, d)
		if err != nil {
			return err
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		return nil
	}
	return fmt.Errorf("unknown migrate direction %q", direction)
}
