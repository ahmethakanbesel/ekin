// Package worker registers the template job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "ekin/internal/domain/job/v1"
	jobsvc "ekin/internal/service/job/v1"
	templatesvc "ekin/internal/service/template/v1"
)

// Register adds the template handlers to r.
func Register(r *jobsvc.Runner, svc *templatesvc.Service) {
	r.Register(templatesvc.KindGenerate, func(ctx context.Context, j *jobdomain.Job) error {
		var p templatesvc.GeneratePayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.Generate(ctx, p)
	})
}
