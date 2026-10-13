// Package worker registers the knowledge job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "kurgu/internal/domain/job/v1"
	jobsvc "kurgu/internal/service/job/v1"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
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
