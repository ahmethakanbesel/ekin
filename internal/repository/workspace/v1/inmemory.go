package workspace

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	workspacedomain "kurgu/internal/domain/workspace/v1"
)

// InMemory is a Repository for service tests. It stores snapshots so a
// caller mutating an aggregate after Create cannot reach into the store.
type InMemory struct {
	mu   sync.RWMutex
	rows map[uuid.UUID]workspacedomain.Snapshot
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory repository.
func NewInMemory() *InMemory {
	return &InMemory{rows: map[uuid.UUID]workspacedomain.Snapshot{}}
}

func (r *InMemory) Create(_ context.Context, w *workspacedomain.Workspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[w.ID()]; ok {
		return apperror.New(apperror.Conflict, "workspace already exists")
	}
	r.rows[w.ID()] = w.Snapshot()
	return nil
}

func (r *InMemory) Update(_ context.Context, w *workspacedomain.Workspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[w.ID()]; !ok {
		return apperror.New(apperror.WorkspaceNotFound, "workspace not found")
	}
	r.rows[w.ID()] = w.Snapshot()
	return nil
}

func (r *InMemory) Get(_ context.Context, id uuid.UUID) (*workspacedomain.Workspace, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.rows[id]
	if !ok {
		return nil, apperror.New(apperror.WorkspaceNotFound, "workspace not found")
	}
	return workspacedomain.Rehydrate(s)
}

func (r *InMemory) List(_ context.Context) ([]*workspacedomain.Workspace, error) {
	r.mu.RLock()
	snaps := make([]workspacedomain.Snapshot, 0, len(r.rows))
	for _, s := range r.rows {
		snaps = append(snaps, s)
	}
	r.mu.RUnlock()
	sort.Slice(snaps, func(i, j int) bool {
		if !snaps[i].CreatedAt.Equal(snaps[j].CreatedAt) {
			return snaps[i].CreatedAt.Before(snaps[j].CreatedAt)
		}
		return snaps[i].ID.String() < snaps[j].ID.String()
	})
	out := make([]*workspacedomain.Workspace, 0, len(snaps))
	for _, s := range snaps {
		w, err := workspacedomain.Rehydrate(s)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}
