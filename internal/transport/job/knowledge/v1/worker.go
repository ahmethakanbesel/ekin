// Package worker registers the knowledge job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	knowledgesvc "github.com/ahmethakanbesel/ekin/internal/service/knowledge/v1"
)

// Register adds the knowledge handlers to r.
func Register(r *jobsvc.Runner, svc *knowledgesvc.Service) {
	r.Register(knowledgesvc.KindExtract, func(ctx context.Context, j *jobdomain.Job) error {
		var p knowledgesvc.ExtractPayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.Extract(ctx, p.DocumentID)
	})
}
