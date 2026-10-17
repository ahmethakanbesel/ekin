// Package worker registers the library job handlers.
package worker

import (
	"context"
	"encoding/json"

	jobdomain "kurgu/internal/domain/job/v1"
	jobsvc "kurgu/internal/service/job/v1"
	librarysvc "kurgu/internal/service/library/v1"
)

// Register adds the library handlers to r.
func Register(r *jobsvc.Runner, svc *librarysvc.Service) {
	r.Register(librarysvc.KindIngest, func(ctx context.Context, j *jobdomain.Job) error {
		var p librarysvc.IngestPayload
		if err := json.Unmarshal(j.Payload(), &p); err != nil {
			return jobdomain.Permanent(err)
		}
		return svc.Ingest(ctx, p.AssetID)
	})
}
