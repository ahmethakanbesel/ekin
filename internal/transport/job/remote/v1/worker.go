// Package worker registers the hybrid worker's handlers: drafts and
// renders run here, state stays on the server.
package worker

import (
	"context"
	"os"

	"kurgu/internal/adapter/remote"
	jobdomain "kurgu/internal/domain/job/v1"
	jobsvc "kurgu/internal/service/job/v1"
	productionsvc "kurgu/internal/service/production/v1"
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
		out, err := os.MkdirTemp("", "kurgu-render-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(out) }()
		files, err := prod.RunRender(ctx, task, dir, out)
		if err != nil {
			return c.Fail(ctx, j.ID(), err)
		}
		return c.FinishRender(ctx, j.ID(), task, files)
	})
}
