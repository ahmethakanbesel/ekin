// Package job persists background jobs.
package job

import (
	"context"
	"time"

	"github.com/google/uuid"

	jobdomain "kurgu/internal/domain/job/v1"
)

// Filter narrows List.
type Filter struct {
	Statuses []jobdomain.Status // empty means all
	Limit    int                // 0 means DefaultListLimit
}

// DefaultListLimit caps an unbounded List.
const DefaultListLimit = 100

// Repository persists Job aggregates.
type Repository interface {
	// Enqueue inserts j unless its unique key is taken, reporting whether
	// it was inserted.
	Enqueue(ctx context.Context, j *jobdomain.Job) (bool, error)
	// Save stores every mutable field of j.
	Save(ctx context.Context, j *jobdomain.Job) error
	// Get returns JobNotFound when id is not in workspaceID.
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*jobdomain.Job, error)
	// GetByID is for workers, which serve every workspace.
	GetByID(ctx context.Context, id uuid.UUID) (*jobdomain.Job, error)
	// List returns a workspace's jobs, newest first.
	List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*jobdomain.Job, error)
	// NextClaimable returns the oldest-due job on one of queues whose kind
	// is in kinds and that jobdomain.Job.Claimable would accept at now, or
	// nil. Call it inside the transaction that starts the job.
	NextClaimable(ctx context.Context, queues []jobdomain.Queue, kinds []string, now time.Time) (*jobdomain.Job, error)
	// ListAbandoned returns running jobs whose last attempt's lease ran out.
	ListAbandoned(ctx context.Context, now time.Time, limit int) ([]*jobdomain.Job, error)
}
