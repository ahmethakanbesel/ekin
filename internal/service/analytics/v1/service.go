// Package analytics connects a workspace to Google Search Console, syncs
// daily search figures per page and per query, and reports them: the
// workspace's top pages and queries, a published story's page before
// against since, and what moved, for the digest.
package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/adapter/searchconsole"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	analyticsdomain "github.com/ahmethakanbesel/ekin/internal/domain/analytics/v1"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	analyticsrepo "github.com/ahmethakanbesel/ekin/internal/repository/analytics/v1"
	productionsvc "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	"github.com/ahmethakanbesel/ekin/pkg/secretbox"
)

// KindSync is the job that syncs one workspace's figures.
const KindSync = "analytics.sync"

// SyncPayload addresses the workspace to sync.
type SyncPayload struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

// Row limits per sync request; a sync pages through at most maxRows per
// dimension.
const (
	pageSize = 25000
	maxRows  = 100000
	// stateTTL bounds an OAuth round trip.
	stateTTL = 15 * time.Minute
)

// Google is the Search Console API (internal/adapter/searchconsole).
type Google interface {
	Configured() bool
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (refresh, access string, err error)
	Access(ctx context.Context, refresh string) (string, error)
	Properties(ctx context.Context, access string) ([]searchconsole.Property, error)
	Analytics(ctx context.Context, access, property string, q searchconsole.Query) ([]searchconsole.Row, error)
}

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// Notifier records what moved (the notify service).
type Notifier interface {
	Notify(ctx context.Context, e productionsvc.Event) error
}

// SiteURL returns the workspace's site address, if it has one.
type SiteURL func(ctx context.Context, workspaceID uuid.UUID) (string, bool)

// Service runs analytics use cases.
type Service struct {
	repo     analyticsrepo.Repository
	tx       db.TxManager
	box      *secretbox.Box
	google   func(ctx context.Context) Google
	jobs     Enqueuer
	notifier Notifier
	site     SiteURL
	log      *slog.Logger
	now      func() time.Time
}

// Deps are the collaborators; Notifier and Site are optional.
type Deps struct {
	Repo     analyticsrepo.Repository
	Tx       db.TxManager
	Box      *secretbox.Box
	Google   func(ctx context.Context) Google
	Jobs     Enqueuer
	Notifier Notifier
	Site     SiteURL
	Log      *slog.Logger
}

// NewService returns a Service.
func NewService(d Deps) *Service {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{repo: d.Repo, tx: d.Tx, box: d.Box, google: d.Google, jobs: d.Jobs, notifier: d.Notifier, site: d.Site,
		log: log, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Configured reports whether the server has Google OAuth credentials.
func (s *Service) Configured(ctx context.Context) bool { return s.google(ctx).Configured() }

type oauthState struct {
	WorkspaceID uuid.UUID `json:"w"`
	UserID      uuid.UUID `json:"u"`
	Expires     int64     `json:"e"`
	Purpose     string    `json:"p"`
}

const statePurpose = "searchconsole"

// ConnectURL starts connecting Search Console to the workspace.
func (s *Service) ConnectURL(ctx context.Context, ws, user uuid.UUID) (string, error) {
	if !s.google(ctx).Configured() {
		return "", apperror.New(apperror.ValidationFailed, "Google sign-in is not set up on this server (the YouTube client id and secret, on System › Credentials)")
	}
	raw, _ := json.Marshal(oauthState{WorkspaceID: ws, UserID: user, Expires: s.now().Add(stateTTL).Unix(), Purpose: statePurpose})
	return s.google(ctx).AuthURL(s.box.Sign(string(raw))), nil
}

func (s *Service) state(state string) (oauthState, bool) {
	raw, err := s.box.Verify(state)
	if err != nil {
		return oauthState{}, false
	}
	var st oauthState
	if json.Unmarshal([]byte(raw), &st) != nil || st.Purpose != statePurpose {
		return oauthState{}, false
	}
	return st, true
}

// StateWorkspace returns the workspace a state was issued for, or Nil.
func (s *Service) StateWorkspace(state string) uuid.UUID {
	st, _ := s.state(state)
	return st.WorkspaceID
}

// Callback finishes the OAuth round trip and stores the connection. When
// one property covers the site (or the account has only one), it is
// chosen and a sync starts. It returns the workspace.
func (s *Service) Callback(ctx context.Context, state, code string) (uuid.UUID, error) {
	st, ok := s.state(state)
	if !ok {
		return uuid.Nil, apperror.New(apperror.BadRequest, "the connection request is invalid; start again")
	}
	if s.now().Unix() > st.Expires {
		return st.WorkspaceID, apperror.New(apperror.BadRequest, "the connection request expired; start again")
	}
	g := s.google(ctx)
	refresh, access, err := g.Exchange(ctx, code)
	if err != nil {
		return st.WorkspaceID, err
	}
	c, err := analyticsdomain.Connect(st.WorkspaceID, s.box.Seal(refresh), st.UserID, s.now())
	if err != nil {
		return st.WorkspaceID, err
	}
	props, err := g.Properties(ctx, access)
	if err != nil {
		return st.WorkspaceID, err
	}
	pick := s.pick(ctx, st.WorkspaceID, props)
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.ClearMetrics(ctx, st.WorkspaceID); err != nil {
			return err
		}
		if pick != "" {
			if _, err := c.Choose(pick, s.now()); err != nil {
				return err
			}
		}
		if err := s.repo.PutConnection(ctx, c); err != nil {
			return err
		}
		if pick != "" {
			return s.enqueue(ctx, st.WorkspaceID, "")
		}
		return nil
	})
	return st.WorkspaceID, err
}

// pick is the property to choose by itself: the one covering the site,
// else the only one; "" when a person must choose.
func (s *Service) pick(ctx context.Context, ws uuid.UUID, props []searchconsole.Property) string {
	if s.site != nil {
		if site, ok := s.site(ctx, ws); ok {
			var match []string
			for _, p := range props {
				if analyticsdomain.Matches(p.URL, site) {
					match = append(match, p.URL)
				}
			}
			if len(match) == 1 {
				return match[0]
			}
		}
	}
	if len(props) == 1 {
		return props[0].URL
	}
	return ""
}

// Status is a workspace's connection, if any.
func (s *Service) Status(ctx context.Context, ws uuid.UUID) (*analyticsdomain.Connection, error) {
	c, err := s.repo.Connection(ctx, ws)
	if apperror.Is(err, apperror.NotFound) {
		return nil, nil
	}
	return c, err
}

// access is a fresh access token for c.
func (s *Service) access(ctx context.Context, c *analyticsdomain.Connection) (string, error) {
	refresh, err := s.box.Open(c.SealedToken())
	if err != nil {
		return "", err
	}
	return s.google(ctx).Access(ctx, refresh)
}

// Properties lists the properties the connected account can read.
func (s *Service) Properties(ctx context.Context, ws uuid.UUID) ([]searchconsole.Property, error) {
	c, err := s.repo.Connection(ctx, ws)
	if err != nil {
		return nil, err
	}
	access, err := s.access(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.google(ctx).Properties(ctx, access)
}

// Choose sets the property to read; one the account cannot read is
// refused. A new property drops the figures kept so far and syncs.
func (s *Service) Choose(ctx context.Context, ws uuid.UUID, property string) (*analyticsdomain.Connection, error) {
	props, err := s.Properties(ctx, ws)
	if err != nil {
		return nil, err
	}
	found := false
	for _, p := range props {
		found = found || p.URL == property
	}
	if !found {
		return nil, apperror.New(apperror.ValidationFailed, "the connected Google account cannot read that property")
	}
	var c *analyticsdomain.Connection
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if c, err = s.repo.Connection(ctx, ws); err != nil {
			return err
		}
		changed, err := c.Choose(property, s.now())
		if err != nil || !changed {
			return err
		}
		if err := s.repo.ClearMetrics(ctx, ws); err != nil {
			return err
		}
		if err := s.repo.PutConnection(ctx, c); err != nil {
			return err
		}
		return s.enqueue(ctx, ws, "")
	})
	return c, err
}

// Disconnect forgets the connection and its figures.
func (s *Service) Disconnect(ctx context.Context, ws uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.ClearMetrics(ctx, ws); err != nil {
			return err
		}
		return s.repo.DeleteConnection(ctx, ws)
	})
}

// SyncNow queues a sync of the workspace.
func (s *Service) SyncNow(ctx context.Context, ws uuid.UUID) error {
	c, err := s.repo.Connection(ctx, ws)
	if err != nil {
		return err
	}
	if c.Property() == "" {
		return apperror.New(apperror.ValidationFailed, "choose a Search Console property first")
	}
	return s.enqueue(ctx, ws, "")
}

// enqueue queues a sync; slot, when set, keeps one per scheduled slot.
func (s *Service) enqueue(ctx context.Context, ws uuid.UUID, slot string) error {
	payload, _ := json.Marshal(SyncPayload{WorkspaceID: ws})
	spec := jobdomain.Spec{WorkspaceID: ws, Queue: jobdomain.QueueIO, Kind: KindSync, Payload: payload, MaxAttempts: 3,
		ConcurrencyKey: KindSync + ":" + ws.String()}
	if slot != "" {
		spec.UniqueKey = KindSync + ":" + ws.String() + ":" + slot
	}
	_, _, err := s.jobs.Enqueue(ctx, spec)
	return err
}

// TriggerDue queues the syncs that are due and returns how many.
func (s *Service) TriggerDue(ctx context.Context) (int, error) {
	all, err := s.repo.Connections(ctx)
	if err != nil {
		return 0, err
	}
	now := s.now()
	n := 0
	for _, c := range all {
		if !c.Due(now) {
			continue
		}
		if err := s.enqueue(ctx, c.WorkspaceID(), analyticsdomain.Day(now).Format(time.DateOnly)); err != nil {
			s.log.WarnContext(ctx, "cannot queue a search console sync", slog.String("workspace", c.WorkspaceID().String()), slog.Any("error", err))
			continue
		}
		n++
	}
	return n, nil
}

// RunSync runs an analytics.sync job: it fetches the connection's window
// of days per page and per query, replaces those days, and tells the
// workspace what moved. A failure Google reports is recorded on the
// connection and ends the job; the next scheduled sync tries again.
func (s *Service) RunSync(ctx context.Context, p SyncPayload) error {
	c, err := s.repo.Connection(ctx, p.WorkspaceID)
	if apperror.Is(err, apperror.NotFound) {
		return jobdomain.Permanent(err) // disconnected meanwhile
	}
	if err != nil {
		return err
	}
	start, end, ok := c.Window(s.now())
	if !ok {
		return nil
	}
	property := c.Property()
	pages, queries, err := s.fetch(ctx, c, start, end)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		var apiErr *searchconsole.Error
		permanent := errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500
		if permanent || strings.Contains(err.Error(), "token request failed") {
			s.record(ctx, p.WorkspaceID, property, func(c *analyticsdomain.Connection) { c.Failed(err.Error(), s.now()) })
			return jobdomain.Permanent(err)
		}
		return err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		c, err := s.repo.Connection(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		if c.Property() != property {
			return nil // another property was chosen meanwhile; its own sync runs
		}
		if err := s.repo.ReplaceMetrics(ctx, p.WorkspaceID, analyticsdomain.DimPage, start, end, pages); err != nil {
			return err
		}
		if err := s.repo.ReplaceMetrics(ctx, p.WorkspaceID, analyticsdomain.DimQuery, start, end, queries); err != nil {
			return err
		}
		c.Synced(end, s.now())
		return s.repo.PutConnection(ctx, c)
	})
	if err != nil {
		return err
	}
	s.reportMoves(ctx, p.WorkspaceID, end)
	return nil
}

func (s *Service) record(ctx context.Context, ws uuid.UUID, property string, fn func(*analyticsdomain.Connection)) {
	err := s.tx.WithinTx(context.WithoutCancel(ctx), func(ctx context.Context) error {
		c, err := s.repo.Connection(ctx, ws)
		if err != nil || c.Property() != property {
			return err
		}
		fn(c)
		return s.repo.PutConnection(ctx, c)
	})
	if err != nil {
		s.log.WarnContext(ctx, "cannot record the sync's outcome", slog.Any("error", err))
	}
}

func (s *Service) fetch(ctx context.Context, c *analyticsdomain.Connection, start, end time.Time) (pages, queries []analyticsdomain.Metric, err error) {
	access, err := s.access(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	get := func(dim analyticsdomain.Dimension) ([]analyticsdomain.Metric, error) {
		var out []analyticsdomain.Metric
		for from := 0; from < maxRows; from += pageSize {
			rows, err := s.google(ctx).Analytics(ctx, access, c.Property(), searchconsole.Query{
				Start: start, End: end, Dimensions: []string{"date", string(dim)}, RowLimit: pageSize, StartRow: from,
			})
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				if len(r.Keys) != 2 {
					continue
				}
				d, err := time.Parse(time.DateOnly, r.Keys[0])
				if err != nil {
					continue
				}
				out = append(out, analyticsdomain.Metric{Date: d, Key: r.Keys[1], Clicks: int(r.Clicks), Impressions: int(r.Impressions),
					Position: r.Position})
			}
			if len(rows) < pageSize {
				break
			}
		}
		return out, nil
	}
	if pages, err = get(analyticsdomain.DimPage); err != nil {
		return nil, nil, err
	}
	if queries, err = get(analyticsdomain.DimQuery); err != nil {
		return nil, nil, err
	}
	return pages, queries, nil
}

// Move thresholds for the digest: a week against the week before.
const (
	moveDays      = 7
	moveMinClicks = 10
	moveLimit     = 5
)

// reportMoves tells the workspace which pages moved most this week.
func (s *Service) reportMoves(ctx context.Context, ws uuid.UUID, end time.Time) {
	if s.notifier == nil {
		return
	}
	ms, err := s.repo.Metrics(ctx, ws, analyticsdomain.DimPage, nil, end.Add(-2*moveDays*24*time.Hour), end)
	if err != nil {
		s.log.WarnContext(ctx, "cannot read search figures for the digest", slog.Any("error", err))
		return
	}
	moves := analyticsdomain.Movers(ms, end, moveDays, moveMinClicks, moveLimit)
	if len(moves) == 0 {
		return
	}
	lines := make([]string, 0, len(moves))
	for _, m := range moves {
		lines = append(lines, fmt.Sprintf("%s %+d clicks (%d → %d)", pagePath(m.Key), m.Delta(), m.Before.Clicks, m.After.Clicks))
	}
	e := productionsvc.Event{WorkspaceID: ws, Kind: "search", Title: "Search clicks moved this week",
		Detail: strings.Join(lines, "; ")}
	if err := s.notifier.Notify(ctx, e); err != nil {
		s.log.WarnContext(ctx, "cannot record what moved", slog.Any("error", err))
	}
}

func pagePath(u string) string {
	if p, err := url.Parse(u); err == nil && p.Path != "" {
		return p.Path
	}
	return u
}

// Overview is the workspace's search figures over a window and the one
// before it.
type Overview struct {
	Start, End        time.Time
	Current, Previous analyticsdomain.Totals
	Pages, Queries    []analyticsrepo.KeyTotals
}

// Overview sums the last days days synced against the days before, with
// the top pages and queries.
func (s *Service) Overview(ctx context.Context, ws uuid.UUID, days, top int) (Overview, error) {
	c, err := s.repo.Connection(ctx, ws)
	if err != nil {
		return Overview{}, err
	}
	end := c.SyncedThrough()
	if end.IsZero() {
		return Overview{}, nil
	}
	days = min(max(days, 1), 90)
	span := time.Duration(days-1) * 24 * time.Hour
	o := Overview{End: end, Start: end.Add(-span)}
	prevEnd := o.Start.Add(-24 * time.Hour)
	ms, err := s.repo.Metrics(ctx, ws, analyticsdomain.DimPage, nil, prevEnd.Add(-span), end)
	if err != nil {
		return o, err
	}
	o.Current = analyticsdomain.Sum(ms, o.Start, o.End)
	o.Previous = analyticsdomain.Sum(ms, prevEnd.Add(-span), prevEnd)
	if o.Pages, err = s.repo.Top(ctx, ws, analyticsdomain.DimPage, o.Start, o.End, top); err != nil {
		return o, err
	}
	o.Queries, err = s.repo.Top(ctx, ws, analyticsdomain.DimQuery, o.Start, o.End, top)
	return o, err
}

// PagePerformance is one page's figures around a publication.
type PagePerformance struct {
	URL     string
	At      time.Time // when it went live
	Through time.Time // the last day synced
	analyticsdomain.Comparison
}

// Published is a page that went live at a moment.
type Published struct {
	URL string
	At  time.Time
}

// Performance compares each page's CompareWindow days before it went
// live with the days since. It returns nothing when Search Console is
// not connected or not synced yet. A page's address matches with or
// without a trailing slash.
func (s *Service) Performance(ctx context.Context, ws uuid.UUID, pages []Published) ([]PagePerformance, error) {
	c, err := s.Status(ctx, ws)
	if err != nil || c == nil || c.SyncedThrough().IsZero() || len(pages) == 0 {
		return nil, err
	}
	through := c.SyncedThrough()
	var out []PagePerformance
	for _, p := range pages {
		keys := []string{p.URL}
		if alt, ok := strings.CutSuffix(p.URL, "/"); ok {
			keys = append(keys, alt)
		} else {
			keys = append(keys, p.URL+"/")
		}
		from := analyticsdomain.Day(p.At).Add(-analyticsdomain.CompareWindow * 24 * time.Hour)
		ms, err := s.repo.Metrics(ctx, ws, analyticsdomain.DimPage, keys, from, through)
		if err != nil {
			return nil, err
		}
		out = append(out, PagePerformance{URL: p.URL, At: p.At, Through: through, Comparison: analyticsdomain.Compare(ms, p.At, through)})
	}
	return out, nil
}

// compile-time check that the adapter satisfies the port.
var _ Google = (*searchconsole.Client)(nil)
