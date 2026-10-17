// Package worker registers the production job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "kurgu/internal/domain/job/v1"
	jobsvc "kurgu/internal/service/job/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	productionsvc "kurgu/internal/service/production/v1"
)

// Register adds the production handlers to r.
func Register(r *jobsvc.Runner, svc *productionsvc.Service) {
	r.Register(pipelinesvc.KindRun, handle(func(ctx context.Context, p pipelinesvc.RunPayload) error {
		return svc.PlanRun(ctx, p)
	}))
	r.Register(productionsvc.KindAdHoc, handle(svc.PlanAdHoc))
	r.Register(productionsvc.KindMoments, handle(svc.FindMoments))
	r.Register(productionsvc.KindDraft, handle(svc.Draft))
	r.Register(productionsvc.KindRender, handle(svc.Render))
	r.Register(productionsvc.KindHarvest, handle(svc.Harvest))
	r.Register(productionsvc.KindPublish, func(ctx context.Context, j *jobdomain.Job) error {
		var p productionsvc.DeliverablePayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.PublishJob(ctx, j.ID(), p)
	})
}

// Watch has jobs settle a deliverable whose step job ended without
// finishing it (svc.Abandoned). Every process that ends jobs needs it:
// the server cancels them and serves remote workers, workers expire
// them.
func Watch(jobs *jobsvc.Service, svc *productionsvc.Service) {
	for _, kind := range []string{productionsvc.KindDraft, productionsvc.KindRender, productionsvc.KindPublish} {
		jobs.OnEnded(kind, func(ctx context.Context, j *jobdomain.Job) error {
			var p productionsvc.DeliverablePayload
			if err := json.Unmarshal(j.Payload(), &p); err != nil {
				return nil // nothing to settle
			}
			reason := j.LastError()
			if j.Status() == jobdomain.StatusCancelled {
				reason = "the job was cancelled"
			}
			return svc.Abandoned(ctx, j.Kind(), j.ID(), p, reason)
		})
	}
}

func handle[P any](fn func(context.Context, P) error) jobsvc.Handler {
	return func(ctx context.Context, j *jobdomain.Job) error {
		var p P
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return fn(ctx, p)
	}
}
