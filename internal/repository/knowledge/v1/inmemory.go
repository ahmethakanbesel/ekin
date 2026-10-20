package knowledge

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	knowledgedomain "github.com/ahmethakanbesel/ekin/internal/domain/knowledge/v1"
	"github.com/ahmethakanbesel/ekin/pkg/fold"
	"github.com/ahmethakanbesel/ekin/pkg/page"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// InMemory is a Repository for service tests.
type InMemory struct {
	mu       sync.RWMutex
	docs     map[uuid.UUID]knowledgedomain.Snapshot
	pages    map[uuid.UUID][]knowledgedomain.Page
	headings map[uuid.UUID][]knowledgedomain.Heading
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory repository.
func NewInMemory() *InMemory {
	return &InMemory{
		docs:     map[uuid.UUID]knowledgedomain.Snapshot{},
		pages:    map[uuid.UUID][]knowledgedomain.Page{},
		headings: map[uuid.UUID][]knowledgedomain.Heading{},
	}
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

func (r *InMemory) List(_ context.Context, workspaceID uuid.UUID, f Filter) ([]*knowledgedomain.Document, string, error) {
	var after cursor
	hasAfter, err := page.Decode(f.After, &after)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	}
	query := fold.String(f.Query)
	r.mu.RLock()
	var snaps []knowledgedomain.Snapshot
	for _, s := range r.docs {
		at := timeutil.ToMillis(s.CreatedAt)
		if s.WorkspaceID != workspaceID ||
			len(f.Statuses) > 0 && !slices.Contains(f.Statuses, knowledgedomain.Status(s.Status)) ||
			len(f.Formats) > 0 && !slices.Contains(f.Formats, knowledgedomain.Format(s.Format)) ||
			query != "" && !strings.Contains(fold.String(s.Title), query) ||
			hasAfter && (at > after.At || at == after.At && s.ID.String() >= after.ID) {
			continue
		}
		snaps = append(snaps, s)
	}
	r.mu.RUnlock()
	slices.SortFunc(snaps, func(a, b knowledgedomain.Snapshot) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID.String(), a.ID.String()))
	})
	out := make([]*knowledgedomain.Document, 0, len(snaps))
	for _, s := range snaps {
		d, err := knowledgedomain.Rehydrate(s)
		if err != nil {
			return nil, "", err
		}
		out = append(out, d)
	}
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	if len(out) <= limit {
		return out, "", nil
	}
	return out[:limit], nextCursor(out[limit-1]), nil
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
	delete(r.headings, id)
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

func (r *InMemory) ReplaceHeadings(_ context.Context, documentID uuid.UUID, hs []knowledgedomain.Heading) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.headings[documentID] = slices.Clone(hs)
	return nil
}

func (r *InMemory) Headings(_ context.Context, documentID uuid.UUID) ([]knowledgedomain.Heading, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.headings[documentID]), nil
}
