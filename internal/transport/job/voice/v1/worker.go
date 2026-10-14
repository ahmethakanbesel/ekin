// Package worker registers the voice job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "kurgu/internal/domain/job/v1"
	jobsvc "kurgu/internal/service/job/v1"
	voicesvc "kurgu/internal/service/voice/v1"
)

// Register adds the voice handlers to r.
func Register(r *jobsvc.Runner, svc *voicesvc.Service) {
	r.Register(voicesvc.KindPreview, func(ctx context.Context, j *jobdomain.Job) error {
		var p voicesvc.PreviewPayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.Preview(ctx, p)
	})
}
