package job

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
)

// InMemory is a Repository for service tests.
type InMemory struct {
	mu   sync.RWMutex
	rows map[uuid.UUID]jobdomain.Snapshot
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory repository.
func NewInMemory() *InMemory {
	return &InMemory{rows: map[uuid.UUID]jobdomain.Snapshot{}}
}

func (r *InMemory) Enqueue(_ context.Context, j *jobdomain.Job) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if key := j.UniqueKey(); key != "" {
		for _, s := range r.rows {
			if s.UniqueKey == key {
				return false, nil
			}
		}
	}
	if _, ok := r.rows[j.ID()]; ok {
		return false, apperror.New(apperror.Conflict, "job already exists")
	}
	r.rows[j.ID()] = j.Snapshot()
	return true, nil
}

func (r *InMemory) Save(_ context.Context, j *jobdomain.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[j.ID()]; !ok {
		return apperror.New(apperror.JobNotFound, "job not found")
	}
	r.rows[j.ID()] = j.Snapshot()
	return nil
}

func (r *InMemory) Get(_ context.Context, workspaceID, id uuid.UUID) (*jobdomain.Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.rows[id]
	if !ok || s.WorkspaceID != workspaceID {
		return nil, apperror.New(apperror.JobNotFound, "job not found")
	}
	return jobdomain.Rehydrate(s)
}

func (r *InMemory) GetByID(_ context.Context, id uuid.UUID) (*jobdomain.Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.rows[id]
	if !ok {
		return nil, apperror.New(apperror.JobNotFound, "job not found")
	}
	return jobdomain.Rehydrate(s)
}

func (r *InMemory) List(_ context.Context, workspaceID uuid.UUID, f Filter) ([]*jobdomain.Job, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	return r.collect(func(j *jobdomain.Job) bool {
		return j.WorkspaceID() == workspaceID && (len(f.Statuses) == 0 || slices.Contains(f.Statuses, j.Status()))
	}, func(a, b *jobdomain.Job) bool {
		if !a.CreatedAt().Equal(b.CreatedAt()) {
			return a.CreatedAt().After(b.CreatedAt())
		}
		return a.ID().String() > b.ID().String()
	}, limit)
}

func (r *InMemory) NextClaimable(_ context.Context, queues []jobdomain.Queue, kinds []string, now time.Time) (*jobdomain.Job, error) {
	got, err := r.collect(func(j *jobdomain.Job) bool {
		return slices.Contains(queues, j.Queue()) && slices.Contains(kinds, j.Kind()) && j.Claimable(now)
	}, func(a, b *jobdomain.Job) bool {
		if !a.RunAfter().Equal(b.RunAfter()) {
			return a.RunAfter().Before(b.RunAfter())
		}
		if !a.CreatedAt().Equal(b.CreatedAt()) {
			return a.CreatedAt().Before(b.CreatedAt())
		}
		return a.ID().String() < b.ID().String()
	}, 1)
	if err != nil || len(got) == 0 {
		return nil, err
	}
	return got[0], nil
}

func (r *InMemory) ListAbandoned(_ context.Context, now time.Time, limit int) ([]*jobdomain.Job, error) {
	return r.collect(func(j *jobdomain.Job) bool { return j.Abandoned(now) },
		func(a, b *jobdomain.Job) bool { return a.LeaseUntil().Before(b.LeaseUntil()) }, limit)
}

func (r *InMemory) collect(keep func(*jobdomain.Job) bool, less func(a, b *jobdomain.Job) bool, limit int) ([]*jobdomain.Job, error) {
	r.mu.RLock()
	var out []*jobdomain.Job
	for _, s := range r.rows {
		j, err := jobdomain.Rehydrate(s)
		if err != nil {
			r.mu.RUnlock()
			return nil, err
		}
		if keep(j) {
			out = append(out, j)
		}
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, k int) bool { return less(out[i], out[k]) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
