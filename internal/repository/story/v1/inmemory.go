package story

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	storydomain "kurgu/internal/domain/story/v1"
)

type revKey struct {
	deliverable uuid.UUID
	number      int
}

// InMemory is a Repository for service tests.
type InMemory struct {
	mu        sync.RWMutex
	stories   map[uuid.UUID]storydomain.Snapshot
	revisions map[revKey]storydomain.Revision
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty repository.
func NewInMemory() *InMemory {
	return &InMemory{stories: map[uuid.UUID]storydomain.Snapshot{}, revisions: map[revKey]storydomain.Revision{}}
}

func (r *InMemory) Create(_ context.Context, s *storydomain.Story) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stories[s.ID()] = s.Snapshot()
	return nil
}

func (r *InMemory) Save(_ context.Context, s *storydomain.Story) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.stories[s.ID()]
	if !ok {
		return notFound()
	}
	if stored.Version != s.Version() {
		return apperror.New(apperror.StaleVersion, "the story changed meanwhile; reload and try again")
	}
	s.Bump()
	r.stories[s.ID()] = s.Snapshot()
	return nil
}

func (r *InMemory) Get(_ context.Context, workspaceID, id uuid.UUID) (*storydomain.Story, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.stories[id]
	if !ok || s.WorkspaceID != workspaceID {
		return nil, notFound()
	}
	return storydomain.Rehydrate(s)
}

func (r *InMemory) GetByID(_ context.Context, id uuid.UUID) (*storydomain.Story, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.stories[id]
	if !ok {
		return nil, notFound()
	}
	return storydomain.Rehydrate(s)
}

func (r *InMemory) List(_ context.Context, workspaceID uuid.UUID, f Filter) ([]*storydomain.Story, error) {
	r.mu.RLock()
	var all []*storydomain.Story
	for _, snap := range r.stories {
		if snap.WorkspaceID != workspaceID || (f.PipelineID != uuid.Nil && snap.PipelineID != f.PipelineID) {
			continue
		}
		s, err := storydomain.Rehydrate(snap)
		if err != nil {
			r.mu.RUnlock()
			return nil, err
		}
		if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, s.Status()) {
			continue
		}
		all = append(all, s)
	}
	r.mu.RUnlock()
	slices.SortFunc(all, func(a, b *storydomain.Story) int {
		return cmp.Or(b.CreatedAt().Compare(a.CreatedAt()), cmp.Compare(b.ID().String(), a.ID().String()))
	})
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (r *InMemory) Delete(_ context.Context, workspaceID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.stories[id]
	if !ok || s.WorkspaceID != workspaceID {
		return notFound()
	}
	delete(r.stories, id)
	return nil
}

func (r *InMemory) PutRevision(_ context.Context, rev storydomain.Revision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := revKey{rev.DeliverableID, rev.Number}
	if old, ok := r.revisions[k]; ok {
		old.Artifacts, old.Notes = maps.Clone(rev.Artifacts), rev.Notes
		r.revisions[k] = old
		return nil
	}
	rev.Artifacts = maps.Clone(rev.Artifacts)
	r.revisions[k] = rev
	return nil
}

func (r *InMemory) Revisions(_ context.Context, deliverableID uuid.UUID) ([]storydomain.Revision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []storydomain.Revision
	for k, rev := range r.revisions {
		if k.deliverable == deliverableID {
			out = append(out, rev)
		}
	}
	slices.SortFunc(out, func(a, b storydomain.Revision) int { return cmp.Compare(a.Number, b.Number) })
	return out, nil
}

func (r *InMemory) Revision(_ context.Context, deliverableID uuid.UUID, number int) (storydomain.Revision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rev, ok := r.revisions[revKey{deliverableID, number}]
	if !ok {
		return storydomain.Revision{}, apperror.New(apperror.NotFound, "revision not found")
	}
	return rev, nil
}

func (r *InMemory) Similar(_ context.Context, workspaceID uuid.UUID, text string, limit int) ([]Match, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Match
	for _, s := range r.stories {
		if s.WorkspaceID != workspaceID {
			continue
		}
		if score := storydomain.Similarity(text, s.Idea.Title); score > 0 {
			out = append(out, Match{StoryID: s.ID, Title: s.Idea.Title, Angle: s.Idea.Angle, Score: score})
		}
	}
	return rank(out, limit), nil
}
