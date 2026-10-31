// Package worker registers the site job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	sitesvc "github.com/ahmethakanbesel/ekin/internal/service/site/v1"
)

// Register adds the site handlers to r.
func Register(r *jobsvc.Runner, svc *sitesvc.Service) {
	r.Register(sitesvc.KindCrawl, func(ctx context.Context, j *jobdomain.Job) error {
		var p sitesvc.CrawlPayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.RunCrawl(ctx, p)
	})
	r.Register(sitesvc.KindCompetitorCheck, func(ctx context.Context, j *jobdomain.Job) error {
		var p sitesvc.CompetitorPayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.RunCompetitorCheck(ctx, p)
	})
}
