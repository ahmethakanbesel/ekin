package knowledge

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"

	knowledgedomain "kurgu/internal/domain/knowledge/v1"
)

// InMemory is a Repository for service tests.
type InMemory struct {
	mu    sync.RWMutex
	docs  map[uuid.UUID]knowledgedomain.Snapshot
	pages map[uuid.UUID][]knowledgedomain.Page
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory repository.
func NewInMemory() *InMemory {
	return &InMemory{docs: map[uuid.UUID]knowledgedomain.Snapshot{}, pages: map[uuid.UUID][]knowledgedomain.Page{}}
}

func (r *InMemory) Create(_ context.Context, d *knowledgedomain.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.docs[d.ID()] = d.Snapshot()
	return nil
}

func (r *InMemory) Update(_ context.Context, d *knowledgedomain.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.docs[d.ID()]
	if !ok || s.WorkspaceID != d.WorkspaceID() {
		return notFound()
	}
	r.docs[d.ID()] = d.Snapshot()
	return nil
}

func (r *InMemory) Get(_ context.Context, workspaceID, id uuid.UUID) (*knowledgedomain.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.docs[id]
	if !ok || s.WorkspaceID != workspaceID {
		return nil, notFound()
	}
	return knowledgedomain.Rehydrate(s)
}

func (r *InMemory) GetByID(_ context.Context, id uuid.UUID) (*knowledgedomain.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.docs[id]
	if !ok {
		return nil, notFound()
	}
	return knowledgedomain.Rehydrate(s)
}

func (r *InMemory) List(_ context.Context, workspaceID uuid.UUID) ([]*knowledgedomain.Document, error) {
	r.mu.RLock()
	var snaps []knowledgedomain.Snapshot
	for _, s := range r.docs {
		if s.WorkspaceID == workspaceID {
			snaps = append(snaps, s)
		}
	}
	r.mu.RUnlock()
	slices.SortFunc(snaps, func(a, b knowledgedomain.Snapshot) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID.String(), a.ID.String()))
	})
	out := make([]*knowledgedomain.Document, 0, len(snaps))
	for _, s := range snaps {
		d, err := knowledgedomain.Rehydrate(s)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (r *InMemory) Delete(_ context.Context, workspaceID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.docs[id]
	if !ok || s.WorkspaceID != workspaceID {
		return notFound()
	}
	delete(r.docs, id)
	delete(r.pages, id)
	return nil
}

func (r *InMemory) ReplacePages(_ context.Context, documentID uuid.UUID, pages []knowledgedomain.Page) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pages[documentID] = slices.Clone(pages)
	return nil
}

func (r *InMemory) Pages(_ context.Context, documentID uuid.UUID, from, to int) ([]knowledgedomain.Page, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []knowledgedomain.Page
	for _, p := range r.pages[documentID] {
		if p.Number >= from && (to == 0 || p.Number <= to) {
			out = append(out, p)
		}
	}
	return out, nil
}
