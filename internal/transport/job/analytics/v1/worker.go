// Package worker registers the analytics job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	analyticssvc "github.com/ahmethakanbesel/ekin/internal/service/analytics/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
)

// Register adds the analytics handlers to r.
func Register(r *jobsvc.Runner, svc *analyticssvc.Service) {
	r.Register(analyticssvc.KindSync, func(ctx context.Context, j *jobdomain.Job) error {
		var p analyticssvc.SyncPayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.RunSync(ctx, p)
	})
}
