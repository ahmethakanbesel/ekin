package pipeline

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
)

// InMemory is a Repository for service tests.
type InMemory struct {
	mu   sync.RWMutex
	rows map[uuid.UUID]pipelinedomain.Snapshot
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory repository.
func NewInMemory() *InMemory {
	return &InMemory{rows: map[uuid.UUID]pipelinedomain.Snapshot{}}
}

func (r *InMemory) Create(_ context.Context, p *pipelinedomain.Pipeline) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[p.ID()]; ok {
		return apperror.New(apperror.Conflict, "pipeline already exists")
	}
	r.rows[p.ID()] = p.Snapshot()
	return nil
}

func (r *InMemory) Update(_ context.Context, p *pipelinedomain.Pipeline) (*pipelinedomain.Pipeline, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.rows[p.ID()]
	if !ok || stored.WorkspaceID != p.WorkspaceID() {
		return nil, apperror.New(apperror.PipelineNotFound, "pipeline not found")
	}
	if stored.Version != p.Version() {
		return nil, apperror.New(apperror.StaleVersion, "pipeline was changed by someone else; reload and retry")
	}
	snap := p.Snapshot()
	snap.CreatedAt = stored.CreatedAt
	snap.Version++
	r.rows[p.ID()] = snap
	return pipelinedomain.Rehydrate(snap)
}

func (r *InMemory) Get(_ context.Context, workspaceID, id uuid.UUID) (*pipelinedomain.Pipeline, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.rows[id]
	if !ok || s.WorkspaceID != workspaceID {
		return nil, apperror.New(apperror.PipelineNotFound, "pipeline not found")
	}
	return pipelinedomain.Rehydrate(s)
}

func (r *InMemory) List(_ context.Context, workspaceID uuid.UUID) ([]*pipelinedomain.Pipeline, error) {
	return r.collect(func(s pipelinedomain.Snapshot) bool { return s.WorkspaceID == workspaceID },
		func(a, b pipelinedomain.Snapshot) bool {
			if !a.CreatedAt.Equal(b.CreatedAt) {
				return a.CreatedAt.Before(b.CreatedAt)
			}
			return a.ID.String() < b.ID.String()
		}, 0)
}

func (r *InMemory) Delete(_ context.Context, workspaceID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.rows[id]
	if !ok || s.WorkspaceID != workspaceID {
		return apperror.New(apperror.PipelineNotFound, "pipeline not found")
	}
	delete(r.rows, id)
	return nil
}

func (r *InMemory) ListDue(_ context.Context, now time.Time, limit int) ([]*pipelinedomain.Pipeline, error) {
	return r.collect(func(s pipelinedomain.Snapshot) bool {
		return s.Status == string(pipelinedomain.StatusActive) && !s.NextRunAt.IsZero() && !s.NextRunAt.After(now)
	}, func(a, b pipelinedomain.Snapshot) bool {
		if !a.NextRunAt.Equal(b.NextRunAt) {
			return a.NextRunAt.Before(b.NextRunAt)
		}
		return a.ID.String() < b.ID.String()
	}, limit)
}

func (r *InMemory) collect(keep func(pipelinedomain.Snapshot) bool, less func(a, b pipelinedomain.Snapshot) bool, limit int) ([]*pipelinedomain.Pipeline, error) {
	r.mu.RLock()
	var snaps []pipelinedomain.Snapshot
	for _, s := range r.rows {
		if keep(s) {
			snaps = append(snaps, s)
		}
	}
	r.mu.RUnlock()
	sort.Slice(snaps, func(i, j int) bool { return less(snaps[i], snaps[j]) })
	if limit > 0 && len(snaps) > limit {
		snaps = snaps[:limit]
	}
	out := make([]*pipelinedomain.Pipeline, 0, len(snaps))
	for _, s := range snaps {
		p, err := pipelinedomain.Rehydrate(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
