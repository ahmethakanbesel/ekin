package site

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// Competitor limits and pace.
const (
	MaxCompetitors     = 10
	CompetitorEvery    = 7 * 24 * time.Hour
	MaxCompetitorName  = 80
	MaxCompetitorPages = 50000
)

// Competitor is a rival site whose sitemaps Ekin compares week by week.
type Competitor struct {
	id          uuid.UUID
	workspaceID uuid.UUID
	name        string
	url         string // its address, normalized like a site's
	pages       int    // pages its sitemaps listed at the last check
	lastCheckAt time.Time
	lastError   string
	nextCheckAt time.Time
	createdAt   time.Time
}

// NewCompetitor starts following a competitor; its first check, which
// only records the pages it has (a baseline), is due at once.
func NewCompetitor(id, ws uuid.UUID, name, address string, now time.Time) (*Competitor, error) {
	if id == uuid.Nil || ws == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "competitor and workspace ids are required")
	}
	if !strings.Contains(address, "://") {
		address = "https://" + strings.TrimSpace(address)
	}
	u, err := ParseURL(address)
	if err != nil {
		return nil, err
	}
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		parsed, _ := url.Parse(u)
		name = strings.TrimPrefix(parsed.Hostname(), "www.")
	}
	if utf8.RuneCountInString(name) > MaxCompetitorName {
		return nil, apperror.New(apperror.ValidationFailed, "the competitor's name is too long")
	}
	now = timeutil.Normalize(now)
	return &Competitor{id: id, workspaceID: ws, name: name, url: u, nextCheckAt: now, createdAt: now}, nil
}

// Baseline reports whether the next check is the first, which records
// the pages without calling any of them new.
func (c *Competitor) Baseline() bool { return c.lastCheckAt.IsZero() }

// Checked records a check that found pages pages.
func (c *Competitor) Checked(pages int, now time.Time) {
	now = timeutil.Normalize(now)
	c.pages, c.lastCheckAt, c.lastError, c.nextCheckAt = pages, now, "", now.Add(CompetitorEvery)
}

// CheckFailed records a failed check; the next waits a whole interval.
func (c *Competitor) CheckFailed(reason string, now time.Time) {
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	now = timeutil.Normalize(now)
	c.lastError, c.nextCheckAt = reason, now.Add(CompetitorEvery)
}

// Due reports whether a check is due at now.
func (c *Competitor) Due(now time.Time) bool { return !now.Before(c.nextCheckAt) }

func (c *Competitor) ID() uuid.UUID          { return c.id }
func (c *Competitor) WorkspaceID() uuid.UUID { return c.workspaceID }
func (c *Competitor) Name() string           { return c.name }
func (c *Competitor) URL() string            { return c.url }
func (c *Competitor) Pages() int             { return c.pages }
func (c *Competitor) LastCheckAt() time.Time { return c.lastCheckAt }
func (c *Competitor) LastError() string      { return c.lastError }
func (c *Competitor) NextCheckAt() time.Time { return c.nextCheckAt }
func (c *Competitor) CreatedAt() time.Time   { return c.createdAt }

// CompetitorSnapshot is a Competitor's stored form.
type CompetitorSnapshot struct {
	ID, WorkspaceID                     uuid.UUID
	Name, URL, LastError                string
	Pages                               int
	LastCheckAt, NextCheckAt, CreatedAt time.Time
}

func (c *Competitor) Snapshot() CompetitorSnapshot {
	return CompetitorSnapshot{ID: c.id, WorkspaceID: c.workspaceID, Name: c.name, URL: c.url, LastError: c.lastError, Pages: c.pages,
		LastCheckAt: c.lastCheckAt, NextCheckAt: c.nextCheckAt, CreatedAt: c.createdAt}
}

// RehydrateCompetitor rebuilds a stored Competitor.
func RehydrateCompetitor(sn CompetitorSnapshot) (*Competitor, error) {
	if sn.ID == uuid.Nil || sn.WorkspaceID == uuid.Nil || sn.Name == "" {
		return nil, apperror.New(apperror.InternalError, "stored competitor is invalid")
	}
	if _, err := ParseURL(sn.URL); err != nil {
		return nil, apperror.New(apperror.InternalError, "stored competitor address is invalid")
	}
	return &Competitor{id: sn.ID, workspaceID: sn.WorkspaceID, name: sn.Name, url: sn.URL, lastError: sn.LastError, pages: sn.Pages,
		lastCheckAt: timeutil.Normalize(sn.LastCheckAt), nextCheckAt: timeutil.Normalize(sn.NextCheckAt), createdAt: timeutil.Normalize(sn.CreatedAt)}, nil
}

// CompetitorPage is a page a competitor's sitemaps listed.
type CompetitorPage struct {
	URL       string
	Title     string
	FirstSeen time.Time
	LastSeen  time.Time
	RemovedAt time.Time // zero while listed
}

// ChangeKind is what happened to a competitor's page.
type ChangeKind string

const (
	PageAdded   ChangeKind = "added"
	PageRemoved ChangeKind = "removed"
)

// CompetitorChange is a page a competitor added or removed, found by a
// check: a signal for what to cover or answer.
type CompetitorChange struct {
	ID           uuid.UUID
	WorkspaceID  uuid.UUID
	CompetitorID uuid.UUID
	Kind         ChangeKind
	URL          string
	Title        string
	At           time.Time
}

// DiffPages compares what a check listed with the pages known so far. It
// returns every page to keep (listed ones seen now, unlisted ones marked
// removed) and the changes: a page newly listed (or listed again after
// it was removed) is added, a known page no longer listed is removed. A
// baseline check records pages and reports no changes.
func DiffPages(known []CompetitorPage, listed []string, baseline bool, ws, competitor uuid.UUID, ids func() uuid.UUID, now time.Time) ([]CompetitorPage, []CompetitorChange) {
	now = timeutil.Normalize(now)
	byURL := make(map[string]CompetitorPage, len(known))
	for _, p := range known {
		byURL[p.URL] = p
	}
	var pages []CompetitorPage
	var changes []CompetitorChange
	change := func(kind ChangeKind, p CompetitorPage) {
		if !baseline {
			changes = append(changes, CompetitorChange{ID: ids(), WorkspaceID: ws, CompetitorID: competitor, Kind: kind, URL: p.URL, Title: p.Title, At: now})
		}
	}
	seen := map[string]bool{}
	for _, u := range listed {
		if seen[u] || len(seen) >= MaxCompetitorPages {
			continue
		}
		seen[u] = true
		p, ok := byURL[u]
		switch {
		case !ok:
			p = CompetitorPage{URL: u, FirstSeen: now}
			change(PageAdded, p)
		case !p.RemovedAt.IsZero():
			p.RemovedAt = time.Time{}
			change(PageAdded, p)
		}
		p.LastSeen = now
		pages = append(pages, p)
	}
	for _, p := range known {
		if seen[p.URL] {
			continue
		}
		if p.RemovedAt.IsZero() {
			p.RemovedAt = now
			change(PageRemoved, p)
		}
		pages = append(pages, p)
	}
	return pages, changes
}
