// Package job persists background jobs.
package job

import (
	"context"
	"time"

	"github.com/google/uuid"

	jobdomain "ekin/internal/domain/job/v1"
)

// Filter narrows List and picks its page. A request (a trace) is kept
// when one of its jobs matches every filter; it is then listed whole.
type Filter struct {
	Statuses []jobdomain.Status // empty means any
	Kinds    []string           // empty means any
	Limit    int                // requests; 0 means DefaultListLimit, at most MaxListLimit
	After    string             // the cursor a previous page ended with
}

// Listing limits, in requests.
const (
	DefaultListLimit = 100
	MaxListLimit     = 500
)

// cursor is the sort key of a page's last request: its newest matching
// job's creation and its trace id.
type cursor struct {
	At    int64  `json:"a"`
	Trace string `json:"t"`
}

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
	// List returns every job of a page of a workspace's requests (traces,
	// latest first by their newest matching job), the jobs newest first,
	// and the cursor of the next page ("" after the last).
	List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*jobdomain.Job, string, error)
	// NextClaimable returns the oldest-due job on one of queues whose kind
	// is in kinds and that jobdomain.Job.Claimable would accept at now, or
	// nil. Call it inside the transaction that starts the job.
	NextClaimable(ctx context.Context, queues []jobdomain.Queue, kinds []string, now time.Time) (*jobdomain.Job, error)
	// ListAbandoned returns running jobs whose last attempt's lease ran out.
	ListAbandoned(ctx context.Context, now time.Time, limit int) ([]*jobdomain.Job, error)
}
