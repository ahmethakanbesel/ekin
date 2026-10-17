// Package worker registers the SEO job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "ekin/internal/domain/job/v1"
	jobsvc "ekin/internal/service/job/v1"
	seosvc "ekin/internal/service/seo/v1"
)

// Register adds the SEO handlers to r.
func Register(r *jobsvc.Runner, svc *seosvc.Service) {
	r.Register(seosvc.KindRefresh, func(ctx context.Context, j *jobdomain.Job) error {
		var p seosvc.RefreshPayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.Refresh(ctx, p)
	})
}
