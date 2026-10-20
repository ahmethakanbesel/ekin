// Package worker registers the hybrid worker's handlers: drafts and
// renders run here, state stays on the server.
package worker

import (
	"context"
	"os"

	"github.com/ahmethakanbesel/ekin/internal/adapter/remote"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	productionsvc "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
)

// Register adds the remote draft and render handlers to r. prod needs
// only its agent, renderer and config; it never touches a database here.
func Register(r *jobsvc.Runner, c *remote.Client, prod *productionsvc.Service) {
	r.Register(productionsvc.KindDraft, func(ctx context.Context, j *jobdomain.Job) error {
		task, ok, err := c.BeginDraft(ctx, j.ID())
		if err != nil || !ok {
			return err
		}
		dir := prod.WorkDir(task.Payload.DeliverableID)
		if err := c.FetchWorkdir(ctx, j.ID(), dir); err != nil {
			return c.Fail(ctx, j.ID(), err)
		}
		res, err := prod.RunDraft(ctx, task, dir)
		if err != nil {
			return c.Fail(ctx, j.ID(), err)
		}
		return c.FinishDraft(ctx, j.ID(), task, res, dir)
	})
	r.Register(productionsvc.KindRender, func(ctx context.Context, j *jobdomain.Job) error {
		task, ok, err := c.BeginRender(ctx, j.ID())
		if err != nil || !ok {
			return err
		}
		dir := prod.WorkDir(task.Payload.DeliverableID)
		if err := c.FetchWorkdir(ctx, j.ID(), dir); err != nil {
			return c.Fail(ctx, j.ID(), err)
		}
		out, err := os.MkdirTemp("", "ekin-render-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(out) }()
		files, err := prod.RunRender(ctx, &task, dir, out)
		if err != nil {
			return c.Fail(ctx, j.ID(), err)
		}
		return c.FinishRender(ctx, j.ID(), task, files)
	})
}
