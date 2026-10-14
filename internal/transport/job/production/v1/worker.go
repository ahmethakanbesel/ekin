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
	r.Register(productionsvc.KindDraft, handle(svc.Draft))
	r.Register(productionsvc.KindRender, handle(svc.Render))
	r.Register(productionsvc.KindPublish, handle(svc.PublishJob))
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
