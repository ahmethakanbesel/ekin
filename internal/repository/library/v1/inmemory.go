package library

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	librarydomain "ekin/internal/domain/library/v1"
	"ekin/pkg/fold"
	"ekin/pkg/page"
	"ekin/pkg/timeutil"
)

// InMemory is a Repository for service tests.
type InMemory struct {
	mu      sync.RWMutex
	assets  map[uuid.UUID]librarydomain.Snapshot
	usages  []librarydomain.Usage
	collect map[uuid.UUID]librarydomain.Collect
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory repository.
func NewInMemory() *InMemory {
	return &InMemory{assets: map[uuid.UUID]librarydomain.Snapshot{}, collect: map[uuid.UUID]librarydomain.Collect{}}
}

func (r *InMemory) Create(_ context.Context, a *librarydomain.Asset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := a.Snapshot()
	for _, o := range r.assets {
		if o.WorkspaceID == s.WorkspaceID && (o.SHA256 == s.SHA256 || s.Harvest.SourceRef != "" && o.Harvest.SourceRef == s.Harvest.SourceRef) {
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
	s.Kind, s.Origin, s.Filename, s.Size, s.SHA256, s.CreatedBy, s.CreatedAt, s.Harvest =
		old.Kind, old.Origin, old.Filename, old.Size, old.SHA256, old.CreatedBy, old.CreatedAt, old.Harvest
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

func (r *InMemory) FindBySourceRef(_ context.Context, workspaceID uuid.UUID, ref string) (*librarydomain.Asset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.assets {
		if s.WorkspaceID == workspaceID && ref != "" && s.Harvest.SourceRef == ref {
			return librarydomain.Rehydrate(s)
		}
	}
	return nil, notFound()
}

func (r *InMemory) Provisional(_ context.Context, workspaceID, deliverableID uuid.UUID) ([]*librarydomain.Asset, error) {
	r.mu.RLock()
	var snaps []librarydomain.Snapshot
	for _, s := range r.assets {
		if s.WorkspaceID == workspaceID && s.Harvest.DeliverableID == deliverableID && s.Provisional {
			snaps = append(snaps, s)
		}
	}
	r.mu.RUnlock()
	slices.SortFunc(snaps, func(a, b librarydomain.Snapshot) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID.String(), b.ID.String()))
	})
	out := make([]*librarydomain.Asset, 0, len(snaps))
	for _, s := range snaps {
		a, err := librarydomain.Rehydrate(s)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (r *InMemory) GetCollect(_ context.Context, workspaceID uuid.UUID) (librarydomain.Collect, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.collect[workspaceID], nil
}

func (r *InMemory) SaveCollect(_ context.Context, workspaceID uuid.UUID, c librarydomain.Collect, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collect[workspaceID] = c
	return nil
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
	r.usages = slices.DeleteFunc(r.usages, func(u librarydomain.Usage) bool { return u.AssetID == id })
	return nil
}

func (r *InMemory) RecordUsage(_ context.Context, u librarydomain.Usage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.assets[u.AssetID]; !ok || s.WorkspaceID != u.WorkspaceID {
		return nil
	}
	for _, o := range r.usages {
		if o.AssetID == u.AssetID && o.DeliverableID == u.DeliverableID && o.Revision == u.Revision {
			return nil
		}
	}
	r.usages = append(r.usages, u)
	return nil
}

func (r *InMemory) Usages(_ context.Context, workspaceID, assetID uuid.UUID) ([]librarydomain.Usage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []librarydomain.Usage
	for _, u := range r.usages {
		if u.WorkspaceID == workspaceID && u.AssetID == assetID {
			out = append(out, u)
		}
	}
	slices.SortFunc(out, func(a, b librarydomain.Usage) int {
		return cmp.Or(b.At.Compare(a.At), cmp.Compare(b.DeliverableID.String(), a.DeliverableID.String()), cmp.Compare(b.Revision, a.Revision))
	})
	return out, nil
}

func (r *InMemory) UsageCounts(_ context.Context, workspaceID uuid.UUID, assetIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[[2]uuid.UUID]bool{}
	out := map[uuid.UUID]int{}
	for _, u := range r.usages {
		key := [2]uuid.UUID{u.AssetID, u.DeliverableID}
		if u.WorkspaceID != workspaceID || !slices.Contains(assetIDs, u.AssetID) || seen[key] {
			continue
		}
		seen[key] = true
		out[u.AssetID]++
	}
	return out, nil
}
