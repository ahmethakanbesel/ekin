package site

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	siterepo "github.com/ahmethakanbesel/ekin/internal/repository/site/v1"
)

// ChangeSite is what a change to the site needs from its settings.
type ChangeSite struct {
	URL   string
	Build string // empty skips the build
}

// ChangeSite returns the site a change is made to; NotFound when the
// workspace has none.
func (s *Service) ChangeSite(ctx context.Context, ws uuid.UUID) (ChangeSite, error) {
	site, err := s.repo.Site(ctx, ws)
	if err != nil {
		return ChangeSite{}, err
	}
	return ChangeSite{URL: site.URL(), Build: site.Build()}, nil
}

// Claim gives the findings ids to story's change and returns them. All
// or none: a finding that is not open, or that another change holds,
// fails the whole claim.
func (s *Service) Claim(ctx context.Context, ws, story uuid.UUID, ids []uuid.UUID) ([]*sitedomain.Finding, error) {
	if len(ids) == 0 {
		return nil, apperror.New(apperror.ValidationFailed, "choose at least one finding")
	}
	var out []*sitedomain.Finding
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		out = out[:0]
		seen := map[uuid.UUID]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			f, err := s.repo.Finding(ctx, ws, id)
			if err != nil {
				return err
			}
			if err := f.Claim(story, s.now()); err != nil {
				code, _ := apperror.GetCode(err)
				return apperror.New(code, fmt.Sprintf("%s on %s: %s", f.Rule(), f.URL(), apperror.Message(err)))
			}
			out = append(out, f)
		}
		return s.repo.PutFindings(ctx, out)
	})
	return out, err
}

// Release gives back the findings story's change held, when the change
// was dropped, and returns how many.
func (s *Service) Release(ctx context.Context, ws, story uuid.UUID) (int, error) {
	n := 0
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		all, err := s.repo.AllFindings(ctx, ws)
		if err != nil {
			return err
		}
		var changed []*sitedomain.Finding
		for _, f := range all {
			if f.Release(story, s.now()) {
				changed = append(changed, f)
			}
		}
		n = len(changed)
		return s.repo.PutFindings(ctx, changed)
	})
	return n, err
}

// NextFindings returns up to limit findings a change could take on
// next: the open, unclaimed findings of the most severe rule that has
// any, by address.
func (s *Service) NextFindings(ctx context.Context, ws uuid.UUID, limit int) ([]*sitedomain.Finding, error) {
	all, err := s.repo.AllFindings(ctx, ws)
	if err != nil {
		return nil, err
	}
	now := s.now()
	all = slices.DeleteFunc(all, func(f *sitedomain.Finding) bool { return !f.Claimable(now) })
	if len(all) == 0 {
		return nil, nil
	}
	// AllFindings comes most severe first, then by rule and address.
	rule := all[0].Rule()
	var out []*sitedomain.Finding
	for _, f := range all {
		if f.Rule() == rule && len(out) < limit {
			out = append(out, f)
		}
	}
	return out, nil
}

// PagesAt returns the inventory's pages at urls, those it has.
func (s *Service) PagesAt(ctx context.Context, ws uuid.UUID, urls []string) ([]sitedomain.Page, error) {
	if len(urls) == 0 {
		return nil, nil
	}
	rows, _, err := s.repo.Pages(ctx, ws, siterepo.PageFilter{URLs: urls, Limit: siterepo.MaxListLimit})
	if err != nil {
		return nil, err
	}
	out := make([]sitedomain.Page, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Page)
	}
	return out, nil
}
