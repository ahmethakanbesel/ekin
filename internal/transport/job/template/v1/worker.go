// Package worker registers the template job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	templatesvc "github.com/ahmethakanbesel/ekin/internal/service/template/v1"
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
