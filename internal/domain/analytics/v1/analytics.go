// Package analytics models what Ekin learns from Google Search Console:
// a workspace's connection to one property, and daily clicks,
// impressions and average position per page and per query. Ekin keeps
// these daily aggregates only, never visitor-level data.
package analytics

import (
	"cmp"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// Sync bounds.
const (
	// Backfill is how far back a first sync reaches, so a story published
	// before the connection still has its 28 days before.
	Backfill = 90 * 24 * time.Hour
	// Settle is how many of the latest days each sync fetches again:
	// Search Console revises recent days for a while.
	Settle = 3 * 24 * time.Hour
	// SyncEvery is how often a connection is synced by itself.
	SyncEvery = 24 * time.Hour
	day       = 24 * time.Hour
)

// Day is t's UTC date at midnight.
func Day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ParseProperty checks a Search Console property: a domain property
// ("sc-domain:example.com") or a URL-prefix one ("https://example.com/").
func ParseProperty(s string) (string, error) {
	s = strings.TrimSpace(s)
	fail := apperror.New(apperror.ValidationFailed, "a property is sc-domain:example.com or an http(s) address ending in /")
	if host, ok := strings.CutPrefix(s, "sc-domain:"); ok {
		if host == "" || strings.ContainsAny(host, "/: ") {
			return "", fail
		}
		return "sc-domain:" + strings.ToLower(host), nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || !strings.HasSuffix(u.Path, "/") {
		return "", fail
	}
	return s, nil
}

// Matches reports whether property covers the site at siteURL: a domain
// property for its host (with or without www), or a URL prefix it starts
// with.
func Matches(property, siteURL string) bool {
	u, err := url.Parse(siteURL)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if d, ok := strings.CutPrefix(property, "sc-domain:"); ok {
		d = strings.TrimPrefix(d, "www.")
		return host == d || strings.HasSuffix(host, "."+d)
	}
	return strings.HasPrefix(strings.TrimRight(siteURL, "/")+"/", property)
}

// Connection is a workspace's Search Console connection. The refresh
// token is sealed (pkg/secretbox); this type never sees it in clear.
type Connection struct {
	workspaceID   uuid.UUID
	sealedToken   string
	property      string // "" until one is chosen
	connectedBy   uuid.UUID
	connectedAt   time.Time
	syncedThrough time.Time // the last day fetched; zero before the first sync
	lastSyncAt    time.Time
	lastError     string
	updatedAt     time.Time
}

// Connect stores a new connection; its property is chosen next.
func Connect(ws uuid.UUID, sealedToken string, by uuid.UUID, now time.Time) (*Connection, error) {
	if ws == uuid.Nil || sealedToken == "" {
		return nil, apperror.New(apperror.ValidationFailed, "workspace and token are required")
	}
	now = timeutil.Normalize(now)
	return &Connection{workspaceID: ws, sealedToken: sealedToken, connectedBy: by, connectedAt: now, updatedAt: now}, nil
}

// Choose sets the property to read and reports whether it changed (the
// metrics kept so far then belong to another property).
func (c *Connection) Choose(property string, now time.Time) (bool, error) {
	p, err := ParseProperty(property)
	if err != nil {
		return false, err
	}
	if p == c.property {
		return false, nil
	}
	c.property, c.syncedThrough, c.lastSyncAt, c.lastError = p, time.Time{}, time.Time{}, ""
	c.updatedAt = timeutil.Normalize(now)
	return true, nil
}

// Window is the days the next sync fetches, through yesterday: from
// Backfill ago on a first sync, else the last Settle days again and
// what is new. ok is false without a property or with nothing to fetch.
func (c *Connection) Window(now time.Time) (start, end time.Time, ok bool) {
	if c.property == "" {
		return time.Time{}, time.Time{}, false
	}
	today := Day(now)
	end = today.Add(-day)
	start = today.Add(-Backfill)
	if !c.syncedThrough.IsZero() {
		if again := Day(c.syncedThrough).Add(-Settle + day); again.After(start) {
			start = again
		}
	}
	return start, end, !start.After(end)
}

// Synced records a sync that fetched through end.
func (c *Connection) Synced(end, now time.Time) {
	now = timeutil.Normalize(now)
	c.syncedThrough, c.lastSyncAt, c.lastError, c.updatedAt = Day(end), now, "", now
}

// Failed records a sync that failed; the next try waits SyncEvery like
// a success, so a broken connection does not hammer Google.
func (c *Connection) Failed(reason string, now time.Time) {
	if len(reason) > 2000 {
		reason = reason[:2000]
	}
	now = timeutil.Normalize(now)
	c.lastSyncAt, c.lastError, c.updatedAt = now, reason, now
}

// Due reports whether a sync is due at now.
func (c *Connection) Due(now time.Time) bool {
	return c.property != "" && (c.lastSyncAt.IsZero() || !now.Before(c.lastSyncAt.Add(SyncEvery)))
}

func (c *Connection) WorkspaceID() uuid.UUID   { return c.workspaceID }
func (c *Connection) SealedToken() string      { return c.sealedToken }
func (c *Connection) Property() string         { return c.property }
func (c *Connection) ConnectedBy() uuid.UUID   { return c.connectedBy }
func (c *Connection) ConnectedAt() time.Time   { return c.connectedAt }
func (c *Connection) SyncedThrough() time.Time { return c.syncedThrough }
func (c *Connection) LastSyncAt() time.Time    { return c.lastSyncAt }
func (c *Connection) LastError() string        { return c.lastError }
func (c *Connection) UpdatedAt() time.Time     { return c.updatedAt }

// Snapshot is a Connection's stored form.
type Snapshot struct {
	WorkspaceID, ConnectedBy                          uuid.UUID
	SealedToken, Property, LastError                  string
	ConnectedAt, SyncedThrough, LastSyncAt, UpdatedAt time.Time
}

func (c *Connection) Snapshot() Snapshot {
	return Snapshot{WorkspaceID: c.workspaceID, ConnectedBy: c.connectedBy, SealedToken: c.sealedToken, Property: c.property,
		LastError: c.lastError, ConnectedAt: c.connectedAt, SyncedThrough: c.syncedThrough, LastSyncAt: c.lastSyncAt, UpdatedAt: c.updatedAt}
}

// Rehydrate rebuilds a stored Connection.
func Rehydrate(sn Snapshot) (*Connection, error) {
	if sn.WorkspaceID == uuid.Nil || sn.SealedToken == "" {
		return nil, apperror.New(apperror.InternalError, "stored search console connection is invalid")
	}
	if sn.Property != "" {
		if _, err := ParseProperty(sn.Property); err != nil {
			return nil, apperror.New(apperror.InternalError, "stored search console property is invalid")
		}
	}
	return &Connection{workspaceID: sn.WorkspaceID, connectedBy: sn.ConnectedBy, sealedToken: sn.SealedToken, property: sn.Property,
		lastError: sn.LastError, connectedAt: timeutil.Normalize(sn.ConnectedAt), syncedThrough: timeutil.Normalize(sn.SyncedThrough),
		lastSyncAt: timeutil.Normalize(sn.LastSyncAt), updatedAt: timeutil.Normalize(sn.UpdatedAt)}, nil
}

// Dimension is what a metric row is about.
type Dimension string

const (
	DimPage  Dimension = "page"
	DimQuery Dimension = "query"
)

// Metric is one day's search figures for a page or a query.
type Metric struct {
	Date        time.Time
	Key         string // the page's URL or the query
	Clicks      int
	Impressions int
	Position    float64 // average position, 1 is the top
}

// Totals sum metrics over days: position is averaged weighted by
// impressions, as Search Console does.
type Totals struct {
	Clicks      int     `json:"clicks"`
	Impressions int     `json:"impressions"`
	Position    float64 `json:"position"` // 0 without impressions
	Days        int     `json:"days"`
}

// CTR is clicks per impression, 0 without impressions.
func (t Totals) CTR() float64 {
	if t.Impressions == 0 {
		return 0
	}
	return float64(t.Clicks) / float64(t.Impressions)
}

// ClicksPerDay evens out windows of different lengths.
func (t Totals) ClicksPerDay() float64 {
	if t.Days == 0 {
		return 0
	}
	return float64(t.Clicks) / float64(t.Days)
}

// Sum totals the metrics dated in [start, end].
func Sum(ms []Metric, start, end time.Time) Totals {
	start, end = Day(start), Day(end)
	t := Totals{}
	if !end.Before(start) {
		t.Days = int(end.Sub(start)/day) + 1
	}
	var weighted float64
	for _, m := range ms {
		d := Day(m.Date)
		if d.Before(start) || d.After(end) {
			continue
		}
		t.Clicks += m.Clicks
		t.Impressions += m.Impressions
		weighted += m.Position * float64(m.Impressions)
	}
	if t.Impressions > 0 {
		t.Position = weighted / float64(t.Impressions)
	}
	return t
}

// CompareWindow is how many days before a publication its page is
// measured over.
const CompareWindow = 28

// Comparison is a page's figures before a moment against since it.
type Comparison struct {
	Before Totals `json:"before"`
	After  Totals `json:"after"`
}

// Compare measures ms over the CompareWindow days before at against the
// days from at through through (the last day synced). Days not synced
// yet count for neither.
func Compare(ms []Metric, at, through time.Time) Comparison {
	at = Day(at)
	return Comparison{
		Before: Sum(ms, at.Add(-CompareWindow*day), at.Add(-day)),
		After:  Sum(ms, at, Day(through)),
	}
}

// Move is how a key's clicks changed between two equal windows.
type Move struct {
	Key           string
	Before, After Totals
}

// Delta is the change in clicks.
func (m Move) Delta() int { return m.After.Clicks - m.Before.Clicks }

// Movers compares the days days ending at end with the days before them
// and returns the keys whose clicks moved by at least minClicks and a
// fifth, biggest moves first, at most limit.
func Movers(ms []Metric, end time.Time, days, minClicks, limit int) []Move {
	end = Day(end)
	afterStart := end.Add(-time.Duration(days-1) * day)
	beforeEnd, beforeStart := afterStart.Add(-day), afterStart.Add(-time.Duration(days)*day)
	byKey := map[string][]Metric{}
	for _, m := range ms {
		byKey[m.Key] = append(byKey[m.Key], m)
	}
	var out []Move
	for k, list := range byKey {
		mv := Move{Key: k, Before: Sum(list, beforeStart, beforeEnd), After: Sum(list, afterStart, end)}
		d := mv.Delta()
		if abs(d) < minClicks || float64(abs(d)) < 0.2*float64(max(mv.Before.Clicks, 1)) {
			continue
		}
		out = append(out, mv)
	}
	slices.SortFunc(out, func(a, b Move) int {
		return cmp.Or(cmp.Compare(abs(b.Delta()), abs(a.Delta())), strings.Compare(a.Key, b.Key))
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
