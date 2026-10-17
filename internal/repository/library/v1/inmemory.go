package library

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	librarydomain "kurgu/internal/domain/library/v1"
	"kurgu/pkg/fold"
	"kurgu/pkg/page"
	"kurgu/pkg/timeutil"
)

// InMemory is a Repository for service tests.
type InMemory struct {
	mu     sync.RWMutex
	assets map[uuid.UUID]librarydomain.Snapshot
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory repository.
func NewInMemory() *InMemory {
	return &InMemory{assets: map[uuid.UUID]librarydomain.Snapshot{}}
}

func (r *InMemory) Create(_ context.Context, a *librarydomain.Asset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := a.Snapshot()
	for _, o := range r.assets {
		if o.WorkspaceID == s.WorkspaceID && o.SHA256 == s.SHA256 {
			return duplicate()
		}
	}
	if _, ok := r.assets[s.ID]; ok {
		return duplicate()
	}
	r.assets[s.ID] = s
	return nil
}

func (r *InMemory) Update(_ context.Context, a *librarydomain.Asset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.assets[a.ID()]
	if !ok || old.WorkspaceID != a.WorkspaceID() {
		return notFound()
	}
	s := a.Snapshot()
	// What Update does not write stays as created.
	s.Kind, s.Origin, s.Filename, s.Size, s.SHA256, s.CreatedBy, s.CreatedAt =
		old.Kind, old.Origin, old.Filename, old.Size, old.SHA256, old.CreatedBy, old.CreatedAt
	r.assets[s.ID] = s
	return nil
}

func (r *InMemory) Get(_ context.Context, workspaceID, id uuid.UUID) (*librarydomain.Asset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.assets[id]
	if !ok || s.WorkspaceID != workspaceID {
		return nil, notFound()
	}
	return librarydomain.Rehydrate(s)
}

func (r *InMemory) GetByID(_ context.Context, id uuid.UUID) (*librarydomain.Asset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.assets[id]
	if !ok {
		return nil, notFound()
	}
	return librarydomain.Rehydrate(s)
}

func (r *InMemory) FindBySHA256(_ context.Context, workspaceID uuid.UUID, sha256 string) (*librarydomain.Asset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.assets {
		if s.WorkspaceID == workspaceID && s.SHA256 == sha256 {
			return librarydomain.Rehydrate(s)
		}
	}
	return nil, notFound()
}

func (r *InMemory) List(_ context.Context, workspaceID uuid.UUID, f Filter) ([]*librarydomain.Asset, string, error) {
	var after cursor
	hasAfter, err := page.Decode(f.After, &after)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	}
	query := fold.String(f.Query)
	r.mu.RLock()
	var out []*librarydomain.Asset
	for _, s := range r.assets {
		a, err := librarydomain.Rehydrate(s)
		if err != nil {
			r.mu.RUnlock()
			return nil, "", err
		}
		at := timeutil.ToMillis(s.CreatedAt)
		text := fold.String(s.Title + " " + strings.Join(s.Tags, " ") + " " + s.Description)
		if s.WorkspaceID != workspaceID ||
			len(f.Kinds) > 0 && !slices.Contains(f.Kinds, a.Kind()) ||
			len(f.Origins) > 0 && !slices.Contains(f.Origins, a.Origin()) ||
			len(f.Statuses) > 0 && !slices.Contains(f.Statuses, a.Status()) ||
			len(f.Orientations) > 0 && !slices.Contains(f.Orientations, a.Orientation()) ||
			!f.Archived && !s.ArchivedAt.IsZero() ||
			query != "" && !strings.Contains(text, query) ||
			hasAfter && (at > after.At || at == after.At && s.ID.String() >= after.ID) {
			continue
		}
		out = append(out, a)
	}
	r.mu.RUnlock()
	slices.SortFunc(out, func(a, b *librarydomain.Asset) int {
		return cmp.Or(b.CreatedAt().Compare(a.CreatedAt()), cmp.Compare(b.ID().String(), a.ID().String()))
	})
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	if len(out) <= limit {
		return out, "", nil
	}
	return out[:limit], nextCursor(out[limit-1]), nil
}

func (r *InMemory) Delete(_ context.Context, workspaceID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.assets[id]
	if !ok || s.WorkspaceID != workspaceID {
		return notFound()
	}
	delete(r.assets, id)
	return nil
}
