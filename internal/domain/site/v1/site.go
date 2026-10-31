// Package site models a workspace's website as Ekin sees it from the
// outside: where it lives and how often it is crawled, each crawl, the
// pages the latest crawl found, and the findings the audit raised about
// them, which stay open, get resolved or come back from crawl to crawl.
package site

import (
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// Page limits per crawl.
const (
	DefaultMaxPages = 500
	MaxMaxPages     = 5000
)

// Schedule is how often a site is crawled by itself.
type Schedule string

const (
	ScheduleOff    Schedule = "off"
	ScheduleDaily  Schedule = "daily"
	ScheduleWeekly Schedule = "weekly"
)

// ParseSchedule parses a schedule; "" reads as weekly.
func ParseSchedule(s string) (Schedule, error) {
	switch sc := Schedule(s); sc {
	case "":
		return ScheduleWeekly, nil
	case ScheduleOff, ScheduleDaily, ScheduleWeekly:
		return sc, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "schedule must be off, daily or weekly")
}

func (s Schedule) every() time.Duration {
	switch s {
	case ScheduleDaily:
		return 24 * time.Hour
	case ScheduleWeekly:
		return 7 * 24 * time.Hour
	}
	return 0
}

// ParseURL checks a site's address and returns it normalized: http or
// https, a host, no credentials, query or fragment, the scheme and host
// lowercased, an empty path read as "/".
func ParseURL(raw string) (string, error) {
	fail := apperror.New(apperror.ValidationFailed, "the site address must be an http(s) URL such as https://example.com")
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fail
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fail
	}
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

// Site is a workspace's website and how it is crawled.
type Site struct {
	workspaceID uuid.UUID
	url         string
	maxPages    int
	schedule    Schedule
	nextCrawlAt time.Time // zero when the schedule is off
	updatedBy   uuid.UUID
	updatedAt   time.Time
}

// Settings are what a person sets on a site.
type Settings struct {
	URL      string
	MaxPages int // 0 means DefaultMaxPages
	Schedule Schedule
}

func (st Settings) parse() (Settings, error) {
	u, err := ParseURL(st.URL)
	if err != nil {
		return st, err
	}
	st.URL = u
	if st.MaxPages == 0 {
		st.MaxPages = DefaultMaxPages
	}
	if st.MaxPages < 1 || st.MaxPages > MaxMaxPages {
		return st, apperror.New(apperror.ValidationFailed, "a crawl can fetch between 1 and 5000 pages")
	}
	if st.Schedule, err = ParseSchedule(string(st.Schedule)); err != nil {
		return st, err
	}
	return st, nil
}

// New sets up a workspace's site. Its first scheduled crawl is one
// interval away: the service crawls it once right away.
func New(ws uuid.UUID, st Settings, by uuid.UUID, now time.Time) (*Site, error) {
	if ws == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "workspace is required")
	}
	st, err := st.parse()
	if err != nil {
		return nil, err
	}
	now = timeutil.Normalize(now)
	s := &Site{workspaceID: ws, url: st.URL, maxPages: st.MaxPages, schedule: st.Schedule, updatedBy: by, updatedAt: now}
	s.Crawled(now)
	return s, nil
}

// Update changes the settings and reports whether the address changed
// (the old pages and findings then describe another site). A changed
// schedule counts from now.
func (s *Site) Update(st Settings, by uuid.UUID, now time.Time) (bool, error) {
	st, err := st.parse()
	if err != nil {
		return false, err
	}
	now = timeutil.Normalize(now)
	moved := st.URL != s.url
	if st.Schedule != s.schedule {
		s.schedule = st.Schedule
		s.Crawled(now)
	}
	s.url, s.maxPages, s.updatedBy, s.updatedAt = st.URL, st.MaxPages, by, now
	return moved, nil
}

// Crawled records that a crawl started at now: the next scheduled one is
// an interval later.
func (s *Site) Crawled(now time.Time) {
	if every := s.schedule.every(); every > 0 {
		s.nextCrawlAt = timeutil.Normalize(now).Add(every)
	} else {
		s.nextCrawlAt = time.Time{}
	}
}

// Due reports whether a scheduled crawl is due at now.
func (s *Site) Due(now time.Time) bool {
	return !s.nextCrawlAt.IsZero() && !now.Before(s.nextCrawlAt)
}

func (s *Site) WorkspaceID() uuid.UUID { return s.workspaceID }
func (s *Site) URL() string            { return s.url }
func (s *Site) MaxPages() int          { return s.maxPages }
func (s *Site) Schedule() Schedule     { return s.schedule }
func (s *Site) NextCrawlAt() time.Time { return s.nextCrawlAt }
func (s *Site) UpdatedBy() uuid.UUID   { return s.updatedBy }
func (s *Site) UpdatedAt() time.Time   { return s.updatedAt }

// Snapshot is a Site's stored form.
type Snapshot struct {
	WorkspaceID uuid.UUID
	URL         string
	MaxPages    int
	Schedule    string
	NextCrawlAt time.Time
	UpdatedBy   uuid.UUID
	UpdatedAt   time.Time
}

func (s *Site) Snapshot() Snapshot {
	return Snapshot{WorkspaceID: s.workspaceID, URL: s.url, MaxPages: s.maxPages, Schedule: string(s.schedule),
		NextCrawlAt: s.nextCrawlAt, UpdatedBy: s.updatedBy, UpdatedAt: s.updatedAt}
}

// Rehydrate rebuilds a stored Site.
func Rehydrate(sn Snapshot) (*Site, error) {
	st, err := Settings{URL: sn.URL, MaxPages: sn.MaxPages, Schedule: Schedule(sn.Schedule)}.parse()
	if err != nil || sn.WorkspaceID == uuid.Nil {
		return nil, apperror.New(apperror.InternalError, "stored site is invalid")
	}
	return &Site{workspaceID: sn.WorkspaceID, url: st.URL, maxPages: st.MaxPages, schedule: st.Schedule,
		nextCrawlAt: timeutil.Normalize(sn.NextCrawlAt), updatedBy: sn.UpdatedBy, updatedAt: timeutil.Normalize(sn.UpdatedAt)}, nil
}
