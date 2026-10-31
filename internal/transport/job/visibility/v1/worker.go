// Package worker registers the AI visibility job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	visibilitysvc "github.com/ahmethakanbesel/ekin/internal/service/visibility/v1"
)

// Register adds the visibility handlers to r.
func Register(r *jobsvc.Runner, svc *visibilitysvc.Service) {
	r.Register(visibilitysvc.KindRun, func(ctx context.Context, j *jobdomain.Job) error {
		var p visibilitysvc.RunPayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.RunCheck(ctx, p)
	})
	r.Register(visibilitysvc.KindDraft, func(ctx context.Context, j *jobdomain.Job) error {
		var p visibilitysvc.DraftPayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.RunDraft(ctx, p)
	})
}
