package analytics_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/adapter/searchconsole"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	analyticsdomain "github.com/ahmethakanbesel/ekin/internal/domain/analytics/v1"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	analyticsrepo "github.com/ahmethakanbesel/ekin/internal/repository/analytics/v1"
	analyticssvc "github.com/ahmethakanbesel/ekin/internal/service/analytics/v1"
	productionsvc "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
	"github.com/ahmethakanbesel/ekin/pkg/secretbox"
)

// fakeGoogle answers like Search Console: two properties, and a page
// whose clicks triple from the 20th of October.
type fakeGoogle struct {
	mu      sync.Mutex
	queries []searchconsole.Query
	fail    error
	refresh string
}

func (*fakeGoogle) Configured() bool { return true }
func (*fakeGoogle) AuthURL(state string) string {
	return "https://accounts.example/auth?state=" + url.QueryEscape(state)
}
func (g *fakeGoogle) Exchange(context.Context, string) (string, string, error) {
	return "refresh-1", "access-1", nil
}
func (g *fakeGoogle) Access(_ context.Context, refresh string) (string, error) {
	g.mu.Lock()
	g.refresh = refresh
	g.mu.Unlock()
	return "access-2", nil
}
func (*fakeGoogle) Properties(context.Context, string) ([]searchconsole.Property, error) {
	return []searchconsole.Property{{URL: "sc-domain:example.com"}, {URL: "https://other.org/"}}, nil
}
func (g *fakeGoogle) Analytics(_ context.Context, _, _ string, q searchconsole.Query) ([]searchconsole.Row, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fail != nil {
		return nil, g.fail
	}
	g.queries = append(g.queries, q)
	var rows []searchconsole.Row
	for d := q.Start; !d.After(q.End); d = d.Add(24 * time.Hour) {
		clicks := 2.0
		if !d.Before(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)) {
			clicks = 6
		}
		key := "https://example.com/blog/x/"
		if q.Dimensions[1] == "query" {
			key = "acme tools"
		}
		rows = append(rows, searchconsole.Row{Keys: []string{d.Format(time.DateOnly), key}, Clicks: clicks, Impressions: 100, Position: 4})
	}
	return rows, nil
}

type jobs struct {
	mu    sync.Mutex
	specs []jobdomain.Spec
}

func (j *jobs) Enqueue(_ context.Context, s jobdomain.Spec) (*jobdomain.Job, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, x := range j.specs {
		if s.UniqueKey != "" && x.UniqueKey == s.UniqueKey {
			return nil, false, nil
		}
	}
	j.specs = append(j.specs, s)
	return nil, true, nil
}

type notes struct{ events []productionsvc.Event }

func (n *notes) Notify(_ context.Context, e productionsvc.Event) error {
	n.events = append(n.events, e)
	return nil
}

type fixture struct {
	svc    *analyticssvc.Service
	google *fakeGoogle
	jobs   *jobs
	notes  *notes
	repo   *analyticsrepo.InMemory
	ws     uuid.UUID
	clock  *time.Time
	state  string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	box, _ := secretbox.New([]byte("analytics-test-secret-0123"))
	clock := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	f := fixture{google: &fakeGoogle{}, jobs: &jobs{}, notes: &notes{}, repo: analyticsrepo.NewInMemory(), ws: id.New(), clock: &clock}
	f.svc = analyticssvc.NewService(analyticssvc.Deps{
		Repo: f.repo, Tx: &db.MemTxManager{}, Box: box, Jobs: f.jobs, Notifier: f.notes,
		Google: func(context.Context) analyticssvc.Google { return f.google },
		Site:   func(context.Context, uuid.UUID) (string, bool) { return "https://www.example.com/", true },
	}).WithClock(func() time.Time { return *f.clock })
	u, err := f.svc.ConnectURL(context.Background(), f.ws, id.New())
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(u)
	f.state = parsed.Query().Get("state")
	return f
}

func (f fixture) runSyncs(t *testing.T) {
	t.Helper()
	for _, s := range f.jobs.specs {
		var p analyticssvc.SyncPayload
		_ = json.Unmarshal(s.Payload, &p)
		if err := f.svc.RunSync(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	f.jobs.specs = nil
}

func TestConnectSyncAndReport(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ws, err := f.svc.Callback(ctx, f.state, "code")
	if err != nil || ws != f.ws {
		t.Fatalf("callback = %v %v", ws, err)
	}
	c, _ := f.svc.Status(ctx, f.ws)
	if c == nil || c.Property() != "sc-domain:example.com" || len(f.jobs.specs) != 1 {
		t.Fatalf("connected: %+v, %d syncs queued", c, len(f.jobs.specs))
	}
	if strings.Contains(c.SealedToken(), "refresh-1") {
		t.Fatal("the refresh token is stored in clear")
	}
	f.runSyncs(t)
	if f.google.refresh != "refresh-1" {
		t.Fatalf("access asked with %q", f.google.refresh)
	}
	c, _ = f.svc.Status(ctx, f.ws)
	if !c.SyncedThrough().Equal(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)) || c.LastError() != "" {
		t.Fatalf("synced through %v (%s)", c.SyncedThrough(), c.LastError())
	}
	if q := f.google.queries[0]; !q.Start.Equal(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)) || q.Dimensions[1] != "page" {
		t.Fatalf("first query = %+v", q)
	}
	// The week (24–30) against the week before (17–23): 42 against 30.
	if len(f.notes.events) != 1 || f.notes.events[0].Kind != "search" || !strings.Contains(f.notes.events[0].Detail, "/blog/x/ +12 clicks (30 → 42)") {
		t.Fatalf("digest events = %+v", f.notes.events)
	}

	perf, err := f.svc.Performance(ctx, f.ws, []analyticssvc.Published{{URL: "https://example.com/blog/x", At: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)}})
	if err != nil || len(perf) != 1 {
		t.Fatalf("performance = %+v %v", perf, err)
	}
	if p := perf[0]; p.Before.Clicks != 56 || p.Before.Days != 28 || p.After.Clicks != 66 || p.After.Days != 11 {
		t.Fatalf("before/after = %+v", p.Comparison)
	}
	o, err := f.svc.Overview(ctx, f.ws, 7, 5)
	if err != nil || o.Current.Clicks != 42 || o.Previous.Clicks != 30 || len(o.Pages) != 1 || len(o.Queries) != 1 || o.Queries[0].Key != "acme tools" {
		t.Fatalf("overview = %+v %v", o, err)
	}

	// A day later only the last days are fetched again.
	*f.clock = f.clock.Add(24 * time.Hour)
	if n, _ := f.svc.TriggerDue(ctx); n != 1 {
		t.Fatalf("due syncs = %d", n)
	}
	if n, _ := f.svc.TriggerDue(ctx); n != 1 || len(f.jobs.specs) != 1 {
		t.Fatalf("a second trigger the same day queued again: %d", len(f.jobs.specs))
	}
	f.google.queries = nil
	f.runSyncs(t)
	if q := f.google.queries[0]; !q.Start.Equal(time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)) || !q.End.Equal(time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("second window %v–%v", q.Start, q.End)
	}

	// Google refusing (revoked access) is recorded and not retried.
	*f.clock = f.clock.Add(24 * time.Hour)
	f.google.fail = &searchconsole.Error{Status: 403, Message: "User does not have sufficient permission"}
	err = f.svc.RunSync(ctx, analyticssvc.SyncPayload{WorkspaceID: f.ws})
	if !errors.Is(err, jobdomain.ErrPermanent) {
		t.Fatalf("refused sync = %v", err)
	}
	if c, _ := f.svc.Status(ctx, f.ws); !strings.Contains(c.LastError(), "sufficient permission") {
		t.Fatalf("last error = %q", c.LastError())
	}

	if _, err := f.svc.Choose(ctx, f.ws, "https://unknown.org/"); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("an unreadable property: %v", err)
	}
	f.google.fail = nil
	if c, err := f.svc.Choose(ctx, f.ws, "https://other.org/"); err != nil || c.Property() != "https://other.org/" || !c.SyncedThrough().IsZero() {
		t.Fatalf("choose another: %v", err)
	}
	if left, _ := f.repo.Metrics(ctx, f.ws, analyticsdomain.DimPage, nil, time.Time{}, *f.clock); len(left) != 0 {
		t.Fatal("the old property's figures remain")
	}
	if err := f.svc.Disconnect(ctx, f.ws); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.svc.Status(ctx, f.ws); c != nil {
		t.Fatal("still connected")
	}
}

func TestCallbackRefusesForeignState(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.svc.Callback(ctx, "forged", "code"); !apperror.Is(err, apperror.BadRequest) {
		t.Fatalf("forged state = %v", err)
	}
	*f.clock = f.clock.Add(time.Hour)
	if _, err := f.svc.Callback(ctx, f.state, "code"); !apperror.Is(err, apperror.BadRequest) {
		t.Fatalf("expired state = %v", err)
	}
}
