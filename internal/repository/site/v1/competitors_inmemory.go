package site

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/page"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

func (r *InMemory) Competitors(_ context.Context, ws uuid.UUID) ([]*sitedomain.Competitor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*sitedomain.Competitor
	for _, sn := range r.competitors {
		if sn.WorkspaceID == ws {
			c, err := sitedomain.RehydrateCompetitor(sn)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b *sitedomain.Competitor) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name())), strings.Compare(a.ID().String(), b.ID().String()))
	})
	return out, nil
}

func (r *InMemory) Competitor(_ context.Context, ws, id uuid.UUID) (*sitedomain.Competitor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sn, ok := r.competitors[id]
	if !ok || sn.WorkspaceID != ws {
		return nil, apperror.New(apperror.NotFound, "competitor not found")
	}
	return sitedomain.RehydrateCompetitor(sn)
}

func (r *InMemory) PutCompetitor(_ context.Context, c *sitedomain.Competitor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, sn := range r.competitors {
		if id != c.ID() && sn.WorkspaceID == c.WorkspaceID() && sn.URL == c.URL() {
			return apperror.New(apperror.Conflict, "that competitor is followed already")
		}
	}
	r.competitors[c.ID()] = c.Snapshot()
	return nil
}

func (r *InMemory) DeleteCompetitor(_ context.Context, ws, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sn, ok := r.competitors[id]
	if !ok || sn.WorkspaceID != ws {
		return apperror.New(apperror.NotFound, "competitor not found")
	}
	delete(r.competitors, id)
	delete(r.competitorPages, id)
	r.changes = slices.DeleteFunc(r.changes, func(c sitedomain.CompetitorChange) bool { return c.CompetitorID == id })
	return nil
}

func (r *InMemory) DueCompetitors(_ context.Context, now time.Time) ([]*sitedomain.Competitor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*sitedomain.Competitor
	for _, sn := range r.competitors {
		c, err := sitedomain.RehydrateCompetitor(sn)
		if err != nil {
			return nil, err
		}
		if c.Due(now) {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b *sitedomain.Competitor) int { return a.NextCheckAt().Compare(b.NextCheckAt()) })
	return out, nil
}

func (r *InMemory) CompetitorPages(_ context.Context, id uuid.UUID) ([]sitedomain.CompetitorPage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := slices.Clone(r.competitorPages[id])
	slices.SortFunc(out, func(a, b sitedomain.CompetitorPage) int { return strings.Compare(a.URL, b.URL) })
	return out, nil
}

func (r *InMemory) ReplaceCompetitorPages(_ context.Context, id uuid.UUID, pages []sitedomain.CompetitorPage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.competitorPages[id] = slices.Clone(pages)
	return nil
}

func (r *InMemory) AddCompetitorChanges(_ context.Context, changes []sitedomain.CompetitorChange) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changes = append(r.changes, changes...)
	return nil
}

func (r *InMemory) CompetitorChanges(_ context.Context, ws uuid.UUID, f ChangeFilter) ([]sitedomain.CompetitorChange, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var after changeCursor
	ok, err := page.Decode(f.After, &after)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	}
	var out []sitedomain.CompetitorChange
	for _, c := range r.changes {
		if c.WorkspaceID != ws || (f.CompetitorID != uuid.Nil && c.CompetitorID != f.CompetitorID) || (f.Kind != "" && c.Kind != f.Kind) {
			continue
		}
		at := timeutil.ToMillis(c.At)
		if ok && (at > after.At || (at == after.At && c.ID.String() >= after.ID)) {
			continue
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b sitedomain.CompetitorChange) int {
		return cmp.Or(b.At.Compare(a.At), strings.Compare(b.ID.String(), a.ID.String()))
	})
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	if len(out) <= limit {
		return out, "", nil
	}
	last := out[limit-1]
	return out[:limit], page.Encode(changeCursor{At: timeutil.ToMillis(last.At), ID: last.ID.String()}), nil
}
