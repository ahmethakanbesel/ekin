package site_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/adapter/crawl"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	siterepo "github.com/ahmethakanbesel/ekin/internal/repository/site/v1"
	sitesvc "github.com/ahmethakanbesel/ekin/internal/service/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

type fakeJobs struct {
	mu    sync.Mutex
	specs []jobdomain.Spec
}

func (f *fakeJobs) Enqueue(_ context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.specs = append(f.specs, spec)
	j, err := jobdomain.New(id.New(), spec, time.Now())
	return j, true, err
}

func (f *fakeJobs) last(t *testing.T) sitesvc.CrawlPayload {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.specs) == 0 {
		t.Fatal("no job enqueued")
	}
	var p sitesvc.CrawlPayload
	_ = json.Unmarshal(f.specs[len(f.specs)-1].Payload, &p)
	return p
}

// fakeCrawler returns result (or err) and records the options.
type fakeCrawler struct {
	result crawl.Result
	err    error
	opts   crawl.Options
	during func() // runs mid-crawl, before Progress
}

func (f *fakeCrawler) Crawl(ctx context.Context, _ string, o crawl.Options) (crawl.Result, error) {
	f.opts = o
	if f.during != nil {
		f.during()
		if o.Progress != nil {
			o.Progress(25)
		}
		if ctx.Err() != nil {
			return crawl.Result{}, ctx.Err()
		}
	}
	return f.result, f.err
}

type fixture struct {
	svc     *sitesvc.Service
	repo    *siterepo.InMemory
	jobs    *fakeJobs
	crawler *fakeCrawler
	ws, by  uuid.UUID
	clock   *time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	clock := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	f := fixture{repo: siterepo.NewInMemory(), jobs: &fakeJobs{}, crawler: &fakeCrawler{}, ws: id.New(), by: id.New(), clock: &clock}
	f.svc = sitesvc.NewService(f.repo, &db.MemTxManager{}, f.jobs, f.crawler, sitesvc.Config{Delay: time.Second}, slog.New(slog.DiscardHandler)).
		WithClock(func() time.Time { return *f.clock })
	return f
}

const root = "https://example.com/"

func htmlPage(path, title string, links ...string) crawl.Page {
	return crawl.Page{URL: root + path, Status: 200, ContentType: "text/html", Title: title, Description: "About " + path,
		Canonical: root + path, Lang: "en", H1: []string{title}, Words: 500, Links: links, InSitemap: true, Depth: 1}
}

func result(pages ...crawl.Page) crawl.Result {
	return crawl.Result{Pages: pages, RobotsFound: true, Sitemaps: []string{root + "sitemap.xml"}, LLMsTxt: true}
}

func TestSetUpCrawlsRightAway(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	s, err := f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: "https://Example.com", MaxPages: 40})
	if err != nil {
		t.Fatal(err)
	}
	if s.URL() != root || !s.NextCrawlAt().Equal(f.clock.Add(7*24*time.Hour)) {
		t.Fatalf("site = %s next %v", s.URL(), s.NextCrawlAt())
	}
	spec := f.jobs.specs[0]
	if spec.Kind != sitesvc.KindCrawl || spec.Queue != jobdomain.QueueIO || spec.ConcurrencyKey != "site:"+f.ws.String() {
		t.Fatalf("job = %+v", spec)
	}
	o, err := f.svc.Overview(ctx, f.ws)
	if err != nil || o.Latest == nil || o.Latest.Trigger() != sitedomain.TriggerSetup || o.Latest.Status() != sitedomain.CrawlQueued || o.LastDone != nil {
		t.Fatalf("overview = %+v %v", o, err)
	}
	if _, err := f.svc.CrawlNow(ctx, f.ws, f.by); !apperror.Is(err, apperror.Conflict) {
		t.Fatalf("crawl while one is queued = %v", err)
	}
	if _, err := f.svc.CrawlNow(ctx, id.New(), f.by); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("crawl without a site = %v", err)
	}
}

func TestRunCrawlAuditsAndReconciles(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: root, MaxPages: 40}); err != nil {
		t.Fatal(err)
	}
	home := htmlPage("", "Home", root+"about")
	home.Types = []string{"Organization"}
	about := htmlPage("about", "", root)
	about.H1 = []string{"About"}
	f.crawler.result = result(home, about)
	if err := f.svc.RunCrawl(ctx, f.jobs.last(t)); err != nil {
		t.Fatal(err)
	}
	if f.crawler.opts.MaxPages != 40 || f.crawler.opts.Delay != time.Second {
		t.Fatalf("options = %+v", f.crawler.opts)
	}
	o, _ := f.svc.Overview(ctx, f.ws)
	if o.LastDone == nil || o.LastDone.Pages() != 2 || !o.LastDone.Facts().RobotsFound {
		t.Fatalf("last done = %+v", o.LastDone)
	}
	if len(o.Open) != 1 || o.Open[0].Rule != sitedomain.RuleTitleMissing {
		t.Fatalf("open = %v", o.Open)
	}
	rows, _, _ := f.svc.Pages(ctx, f.ws, sitesvc.PageFilter{})
	if len(rows) != 2 || rows[1].OpenFindings != 1 || !rows[1].FirstSeen.Equal(*f.clock) {
		t.Fatalf("pages = %+v", rows)
	}

	// A day later the title is fixed: the finding resolves and the
	// pages keep when they were first seen.
	*f.clock = f.clock.Add(24 * time.Hour)
	about.Title = "About"
	f.crawler.result = result(home, about)
	if _, err := f.svc.CrawlNow(ctx, f.ws, f.by); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RunCrawl(ctx, f.jobs.last(t)); err != nil {
		t.Fatal(err)
	}
	o, _ = f.svc.Overview(ctx, f.ws)
	if len(o.Open) != 0 {
		t.Fatalf("open after the fix = %v", o.Open)
	}
	resolved, _, _ := f.svc.Findings(ctx, f.ws, sitesvc.FindingFilter{Statuses: []sitedomain.FindingStatus{sitedomain.FindingResolved}})
	if len(resolved) != 1 || !resolved[0].ResolvedAt().Equal(*f.clock) {
		t.Fatalf("resolved = %v", resolved)
	}
	rows, _, _ = f.svc.Pages(ctx, f.ws, sitesvc.PageFilter{})
	if !rows[1].FirstSeen.Equal(f.clock.Add(-24*time.Hour)) || !rows[1].LastSeen.Equal(*f.clock) {
		t.Fatalf("seen = %v .. %v", rows[1].FirstSeen, rows[1].LastSeen)
	}
}

func TestRunCrawlFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("an unreachable site fails the crawl, not the job", func(t *testing.T) {
		f := newFixture(t)
		_, _ = f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: root})
		f.crawler.err = errors.New("dial tcp: connection refused")
		if err := f.svc.RunCrawl(ctx, f.jobs.last(t)); err != nil {
			t.Fatalf("job error = %v", err)
		}
		o, _ := f.svc.Overview(ctx, f.ws)
		if o.Latest.Status() != sitedomain.CrawlFailed || o.Latest.Failure() != "dial tcp: connection refused" {
			t.Fatalf("latest = %s %q", o.Latest.Status(), o.Latest.Failure())
		}
		if _, err := f.svc.CrawlNow(ctx, f.ws, f.by); err != nil {
			t.Fatalf("crawl again after a failure = %v", err)
		}
	})
	t.Run("an internal address is explained", func(t *testing.T) {
		f := newFixture(t)
		_, _ = f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: root})
		f.crawler.err = errors.Join(crawl.ErrStart, crawl.ErrPrivateAddress)
		_ = f.svc.RunCrawl(ctx, f.jobs.last(t))
		o, _ := f.svc.Overview(ctx, f.ws)
		if o.Latest.Status() != sitedomain.CrawlFailed || len(o.Latest.Failure()) < 40 {
			t.Fatalf("latest = %q", o.Latest.Failure())
		}
	})
	t.Run("a cancelled job leaves the crawl for its retry", func(t *testing.T) {
		f := newFixture(t)
		_, _ = f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: root})
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		f.crawler.err = context.Canceled
		if err := f.svc.RunCrawl(cctx, f.jobs.last(t)); !errors.Is(err, context.Canceled) {
			t.Fatalf("job error = %v", err)
		}
		f.crawler.err = nil
		f.crawler.result = result(htmlPage("", "Home"))
		if err := f.svc.RunCrawl(ctx, f.jobs.last(t)); err != nil {
			t.Fatal(err)
		}
		if o, _ := f.svc.Overview(ctx, f.ws); o.Latest.Status() != sitedomain.CrawlDone {
			t.Fatalf("after the retry the crawl is %s", o.Latest.Status())
		}
	})
	t.Run("a finished crawl's job does nothing", func(t *testing.T) {
		f := newFixture(t)
		_, _ = f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: root})
		f.crawler.result = result(htmlPage("", "Home"))
		p := f.jobs.last(t)
		_ = f.svc.RunCrawl(ctx, p)
		f.crawler.result = crawl.Result{}
		if err := f.svc.RunCrawl(ctx, p); err != nil {
			t.Fatal(err)
		}
		if rows, _, _ := f.svc.Pages(ctx, f.ws, sitesvc.PageFilter{}); len(rows) != 1 {
			t.Fatalf("a rerun replaced the pages: %d", len(rows))
		}
	})
}

func TestMovingTheSiteReplacesTheCrawl(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	_, _ = f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: root})
	f.crawler.result = result(htmlPage("", ""))
	_ = f.svc.RunCrawl(ctx, f.jobs.last(t))
	if o, _ := f.svc.Overview(ctx, f.ws); len(o.Open) == 0 {
		t.Fatal("expected findings on the first site")
	}
	_, _ = f.svc.CrawlNow(ctx, f.ws, f.by)
	old := f.jobs.last(t)

	// The site moves while the old crawl runs: its worker stops and
	// leaves nothing behind.
	f.crawler.during = func() {
		if _, err := f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: "https://new.example"}); err != nil {
			t.Error(err)
		}
	}
	if err := f.svc.RunCrawl(ctx, old); err != nil {
		t.Fatal(err)
	}
	f.crawler.during = nil
	o, _ := f.svc.Overview(ctx, f.ws)
	if o.Site.URL() != "https://new.example/" || len(o.Open) != 0 || o.Latest.Trigger() != sitedomain.TriggerSetup || o.Latest.Status() != sitedomain.CrawlQueued {
		t.Fatalf("after moving: %s, open %v, latest %s %s", o.Site.URL(), o.Open, o.Latest.Trigger(), o.Latest.Status())
	}
	if rows, _, _ := f.svc.Pages(ctx, f.ws, sitesvc.PageFilter{}); len(rows) != 0 {
		t.Fatalf("the old site's pages stayed: %d", len(rows))
	}
	crawls, _ := f.svc.Crawls(ctx, f.ws, 10)
	if crawls[1].Status() != sitedomain.CrawlFailed {
		t.Fatalf("the replaced crawl is %s", crawls[1].Status())
	}
}

func TestTriggerDue(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	_, _ = f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: root, Schedule: sitedomain.ScheduleDaily})
	f.crawler.result = result(htmlPage("", "Home"))
	_ = f.svc.RunCrawl(ctx, f.jobs.last(t))
	if n, err := f.svc.TriggerDue(ctx); err != nil || n != 0 {
		t.Fatalf("not due yet: %d %v", n, err)
	}
	*f.clock = f.clock.Add(25 * time.Hour)
	if n, err := f.svc.TriggerDue(ctx); err != nil || n != 1 {
		t.Fatalf("due: %d %v", n, err)
	}
	o, _ := f.svc.Overview(ctx, f.ws)
	if o.Latest.Trigger() != sitedomain.TriggerSchedule || !o.Site.NextCrawlAt().Equal(f.clock.Add(24*time.Hour)) {
		t.Fatalf("latest %s, next %v", o.Latest.Trigger(), o.Site.NextCrawlAt())
	}
	// A busy worker starts that crawl 23 hours later; it is still
	// running when the next one is due, which waits a turn.
	*f.clock = f.clock.Add(23 * time.Hour)
	c, _ := f.repo.Crawl(ctx, f.ws, o.Latest.ID())
	_ = c.Start(*f.clock)
	_ = f.repo.SaveCrawl(ctx, c)
	*f.clock = f.clock.Add(2 * time.Hour)
	if n, _ := f.svc.TriggerDue(ctx); n != 0 {
		t.Fatalf("started over a running crawl: %d", n)
	}
	if o, _ := f.svc.Overview(ctx, f.ws); !o.Site.NextCrawlAt().Equal(f.clock.Add(24*time.Hour)) || o.Latest.ID() != c.ID() {
		t.Fatalf("next not moved on: %v", o.Site.NextCrawlAt())
	}
	// A crawl running for over three hours stopped without saying so.
	*f.clock = f.clock.Add(25 * time.Hour)
	if n, _ := f.svc.TriggerDue(ctx); n != 1 {
		t.Fatalf("did not replace a stale crawl: %d", n)
	}
	if old, _ := f.repo.Crawl(ctx, f.ws, c.ID()); old.Status() != sitedomain.CrawlFailed {
		t.Fatalf("the stale crawl is %s", old.Status())
	}
}

func TestIgnoreFinding(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	_, _ = f.svc.SaveSite(ctx, f.ws, f.by, sitedomain.Settings{URL: root})
	f.crawler.result = result(htmlPage("", "")) // no title
	_ = f.svc.RunCrawl(ctx, f.jobs.last(t))
	list, _, _ := f.svc.Findings(ctx, f.ws, sitesvc.FindingFilter{Rule: sitedomain.RuleTitleMissing})
	if len(list) != 1 {
		t.Fatalf("findings = %v", list)
	}
	got, err := f.svc.SetFindingStatus(ctx, f.ws, list[0].ID(), sitedomain.FindingIgnored)
	if err != nil || got.Status() != sitedomain.FindingIgnored {
		t.Fatalf("ignore = %v", err)
	}
	if o, _ := f.svc.Overview(ctx, f.ws); slices.ContainsFunc(o.Open, func(c siterepo.RuleCount) bool { return c.Rule == sitedomain.RuleTitleMissing }) {
		t.Fatal("an ignored finding still counts as open")
	}
	if _, err := f.svc.SetFindingStatus(ctx, id.New(), list[0].ID(), sitedomain.FindingOpen); !apperror.Is(err, apperror.NotFound) {
		t.Fatalf("another workspace changed it: %v", err)
	}
	if _, err := f.svc.SetFindingStatus(ctx, f.ws, list[0].ID(), sitedomain.FindingResolved); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("resolved by hand: %v", err)
	}
}
