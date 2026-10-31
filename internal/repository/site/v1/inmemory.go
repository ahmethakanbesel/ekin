package site

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/fold"
	"github.com/ahmethakanbesel/ekin/pkg/page"
)

// InMemory is a Repository for service tests.
type InMemory struct {
	mu       sync.RWMutex
	sites    map[uuid.UUID]sitedomain.Snapshot
	crawls   map[uuid.UUID]sitedomain.CrawlSnapshot
	pages    map[uuid.UUID][]sitedomain.Page
	findings map[uuid.UUID]sitedomain.FindingSnapshot
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty repository.
func NewInMemory() *InMemory {
	return &InMemory{sites: map[uuid.UUID]sitedomain.Snapshot{}, crawls: map[uuid.UUID]sitedomain.CrawlSnapshot{},
		pages: map[uuid.UUID][]sitedomain.Page{}, findings: map[uuid.UUID]sitedomain.FindingSnapshot{}}
}

func (r *InMemory) Site(_ context.Context, ws uuid.UUID) (*sitedomain.Site, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sn, ok := r.sites[ws]
	if !ok {
		return nil, apperror.New(apperror.NotFound, "no site is set up")
	}
	return sitedomain.Rehydrate(sn)
}

func (r *InMemory) PutSite(_ context.Context, s *sitedomain.Site) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sites[s.WorkspaceID()] = s.Snapshot()
	return nil
}

func (r *InMemory) DueSites(_ context.Context, now time.Time) ([]*sitedomain.Site, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*sitedomain.Site
	for _, sn := range r.sites {
		s, err := sitedomain.Rehydrate(sn)
		if err != nil {
			return nil, err
		}
		if s.Due(now) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b *sitedomain.Site) int { return a.NextCrawlAt().Compare(b.NextCrawlAt()) })
	return out, nil
}

func (r *InMemory) CreateCrawl(_ context.Context, c *sitedomain.Crawl) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.crawls[c.ID()]; ok {
		return apperror.New(apperror.Conflict, "crawl exists")
	}
	r.crawls[c.ID()] = c.Snapshot()
	return nil
}

func (r *InMemory) SaveCrawl(_ context.Context, c *sitedomain.Crawl) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.crawls[c.ID()]
	if !ok || old.WorkspaceID != c.WorkspaceID() {
		return apperror.New(apperror.NotFound, "crawl not found")
	}
	r.crawls[c.ID()] = c.Snapshot()
	return nil
}

func (r *InMemory) Crawl(_ context.Context, ws, id uuid.UUID) (*sitedomain.Crawl, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sn, ok := r.crawls[id]
	if !ok || sn.WorkspaceID != ws {
		return nil, apperror.New(apperror.NotFound, "crawl not found")
	}
	return sitedomain.RehydrateCrawl(sn)
}

func (r *InMemory) Crawls(_ context.Context, ws uuid.UUID, limit int) ([]*sitedomain.Crawl, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var sns []sitedomain.CrawlSnapshot
	for _, sn := range r.crawls {
		if sn.WorkspaceID == ws {
			sns = append(sns, sn)
		}
	}
	slices.SortFunc(sns, func(a, b sitedomain.CrawlSnapshot) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(b.ID.String(), a.ID.String()))
	})
	if n := page.Limit(limit, 20, MaxListLimit); len(sns) > n {
		sns = sns[:n]
	}
	out := make([]*sitedomain.Crawl, 0, len(sns))
	for _, sn := range sns {
		c, err := sitedomain.RehydrateCrawl(sn)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *InMemory) ReplacePages(_ context.Context, ws uuid.UUID, pages []sitedomain.Page) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pages[ws] = slices.Clone(pages)
	return nil
}

func (r *InMemory) FirstSeen(_ context.Context, ws uuid.UUID) (map[string]time.Time, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string]time.Time{}
	for _, p := range r.pages[ws] {
		out[p.URL] = p.FirstSeen
	}
	return out, nil
}

func (r *InMemory) Pages(_ context.Context, ws uuid.UUID, f PageFilter) ([]PageRow, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var after pageCursor
	if _, err := page.Decode(f.After, &after); err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	}
	q := fold.String(f.Query)
	var out []PageRow
	for _, p := range r.pages[ws] {
		if (f.Class != "" && p.Class() != f.Class) || (q != "" && !strings.Contains(fold.String(p.URL+" "+p.Title), q)) ||
			(after.URL != "" && p.URL <= after.URL) || (len(f.URLs) > 0 && !slices.Contains(f.URLs, p.URL)) {
			continue
		}
		row := PageRow{Page: p}
		for _, sn := range r.findings {
			if sn.WorkspaceID == ws && sn.URL == p.URL && sn.Status == string(sitedomain.FindingOpen) {
				row.OpenFindings++
			}
		}
		out = append(out, row)
	}
	slices.SortFunc(out, func(a, b PageRow) int { return strings.Compare(a.URL, b.URL) })
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	if len(out) <= limit {
		return out, "", nil
	}
	return out[:limit], page.Encode(pageCursor{URL: out[limit-1].URL}), nil
}

func (r *InMemory) AllFindings(_ context.Context, ws uuid.UUID) ([]*sitedomain.Finding, error) {
	return r.filter(ws, FindingFilter{})
}

func (r *InMemory) PutFindings(_ context.Context, fs []*sitedomain.Finding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range fs {
		for id, sn := range r.findings {
			if id != f.ID() && sn.WorkspaceID == f.WorkspaceID() && sn.Rule == string(f.Rule()) && sn.URL == f.URL() {
				return apperror.New(apperror.Conflict, "a finding for this rule and page exists")
			}
		}
		r.findings[f.ID()] = f.Snapshot()
	}
	return nil
}

func (r *InMemory) Finding(_ context.Context, ws, id uuid.UUID) (*sitedomain.Finding, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sn, ok := r.findings[id]
	if !ok || sn.WorkspaceID != ws {
		return nil, apperror.New(apperror.NotFound, "finding not found")
	}
	return sitedomain.RehydrateFinding(sn)
}

func (r *InMemory) Findings(_ context.Context, ws uuid.UUID, f FindingFilter) ([]*sitedomain.Finding, string, error) {
	var after findingCursor
	ok, err := page.Decode(f.After, &after)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	}
	all, err := r.filter(ws, f)
	if err != nil {
		return nil, "", err
	}
	if ok {
		all = slices.DeleteFunc(all, func(x *sitedomain.Finding) bool {
			return cmp.Or(cmp.Compare(x.Severity().Rank(), after.Rank), strings.Compare(string(x.Rule()), after.Rule),
				strings.Compare(x.URL(), after.URL)) <= 0
		})
	}
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	if len(all) <= limit {
		return all, "", nil
	}
	return all[:limit], encodeFinding(all[limit-1]), nil
}

// filter returns the findings f matches, in listing order.
func (r *InMemory) filter(ws uuid.UUID, f FindingFilter) ([]*sitedomain.Finding, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*sitedomain.Finding
	for _, sn := range r.findings {
		if sn.WorkspaceID != ws {
			continue
		}
		x, err := sitedomain.RehydrateFinding(sn)
		if err != nil {
			return nil, err
		}
		if (len(f.Statuses) > 0 && !slices.Contains(f.Statuses, x.Status())) || (f.Rule != "" && x.Rule() != f.Rule) ||
			(f.Severity != "" && x.Severity() != f.Severity) || (f.URL != "" && x.URL() != f.URL) {
			continue
		}
		out = append(out, x)
	}
	slices.SortFunc(out, func(a, b *sitedomain.Finding) int {
		return cmp.Or(cmp.Compare(a.Severity().Rank(), b.Severity().Rank()), strings.Compare(string(a.Rule()), string(b.Rule())),
			strings.Compare(a.URL(), b.URL()))
	})
	return out, nil
}

func (r *InMemory) RuleCounts(_ context.Context, ws uuid.UUID, status sitedomain.FindingStatus) ([]RuleCount, error) {
	all, err := r.filter(ws, FindingFilter{Statuses: []sitedomain.FindingStatus{status}})
	if err != nil {
		return nil, err
	}
	var out []RuleCount
	for _, f := range all {
		if n := len(out); n > 0 && out[n-1].Rule == f.Rule() {
			out[n-1].Count++
			continue
		}
		out = append(out, RuleCount{Rule: f.Rule(), Count: 1})
	}
	return out, nil
}

func (r *InMemory) ClearInventory(_ context.Context, ws uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pages, ws)
	for id, sn := range r.findings {
		if sn.WorkspaceID == ws {
			delete(r.findings, id)
		}
	}
	return nil
}
