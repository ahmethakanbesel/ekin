package site_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	site "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

var now = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

func TestParseURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://Example.com":      "https://example.com/",
		"http://example.com/docs/": "http://example.com/docs/",
		"HTTPS://EXAMPLE.COM/a":    "https://example.com/a",
	} {
		if got, err := site.ParseURL(raw); err != nil || got != want {
			t.Errorf("ParseURL(%q) = %q %v, want %q", raw, got, err, want)
		}
	}
	for _, bad := range []string{"", "example.com", "ftp://example.com", "https://u:p@example.com", "https://example.com/?q=1", "https://example.com/#x"} {
		if _, err := site.ParseURL(bad); !apperror.Is(err, apperror.ValidationFailed) {
			t.Errorf("ParseURL(%q) = %v, want a validation error", bad, err)
		}
	}
}

func TestSiteSettingsAndSchedule(t *testing.T) {
	ws, by := id.New(), id.New()
	s, err := site.New(ws, site.Settings{URL: "https://example.com"}, by, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.MaxPages() != site.DefaultMaxPages || s.Schedule() != site.ScheduleWeekly || !s.NextCrawlAt().Equal(now.Add(7*24*time.Hour)) {
		t.Fatalf("defaults = %d %s %v", s.MaxPages(), s.Schedule(), s.NextCrawlAt())
	}
	if s.Due(now.Add(time.Hour)) || !s.Due(now.Add(8*24*time.Hour)) {
		t.Fatal("weekly schedule not due when it should be, or due too early")
	}
	moved, err := s.Update(site.Settings{URL: "https://example.com/", MaxPages: 50, Schedule: site.ScheduleDaily}, by, now.Add(time.Hour))
	if err != nil || moved || s.MaxPages() != 50 || !s.NextCrawlAt().Equal(now.Add(25*time.Hour)) {
		t.Fatalf("update = %v %v, next %v", moved, err, s.NextCrawlAt())
	}
	if moved, _ := s.Update(site.Settings{URL: "https://other.example", Schedule: site.ScheduleOff}, by, now); !moved || !s.NextCrawlAt().IsZero() || s.Due(now.Add(1000*time.Hour)) {
		t.Fatalf("moved = %v, next %v", moved, s.NextCrawlAt())
	}
	for _, bad := range []site.Settings{{URL: "nope"}, {URL: "https://x.com", MaxPages: 6000}, {URL: "https://x.com", MaxPages: -1}, {URL: "https://x.com", Schedule: "hourly"}} {
		if _, err := s.Update(bad, by, now); !apperror.Is(err, apperror.ValidationFailed) {
			t.Errorf("Update(%+v) = %v", bad, err)
		}
	}
	back, err := site.Rehydrate(s.Snapshot())
	if err != nil || back.URL() != "https://other.example/" || back.Schedule() != site.ScheduleOff {
		t.Fatalf("rehydrate = %+v %v", back, err)
	}
}

func TestCrawlLifecycle(t *testing.T) {
	c, err := site.NewCrawl(id.New(), id.New(), "https://example.com/", site.TriggerManual, id.New(), now)
	if err != nil || !c.Active() || c.Status() != site.CrawlQueued {
		t.Fatalf("new = %v %v", c, err)
	}
	if err := c.Finish(site.Facts{}, 1, now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("finish before start = %v", err)
	}
	_ = c.Start(now)
	c.Progress(25)
	c.Progress(10)
	if c.Pages() != 25 {
		t.Fatalf("progress went back to %d", c.Pages())
	}
	if err := c.Start(now); err != nil || c.Pages() != 0 {
		t.Fatalf("restart after a worker stopped = %v, pages %d", err, c.Pages())
	}
	facts := site.Facts{RobotsFound: true, Sitemaps: []string{"https://example.com/sitemap.xml"}, AIBlocked: []string{"GPTBot"}}
	if err := c.Finish(facts, 42, now.Add(time.Minute)); err != nil || c.Active() || c.Pages() != 42 {
		t.Fatalf("finish = %v", err)
	}
	if err := c.Fail("late", now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("fail after done = %v", err)
	}
	back, err := site.RehydrateCrawl(c.Snapshot())
	if err != nil || back.Status() != site.CrawlDone || !slices.Equal(back.Facts().AIBlocked, []string{"GPTBot"}) {
		t.Fatalf("rehydrate = %v", err)
	}
	failed, _ := site.NewCrawl(id.New(), id.New(), "https://example.com/", site.TriggerSchedule, id.New(), now)
	if err := failed.Fail("connection refused", now); err != nil || failed.Status() != site.CrawlFailed || failed.Failure() != "connection refused" {
		t.Fatalf("fail queued = %v", err)
	}
	if _, err := site.NewCrawl(id.New(), id.New(), "https://example.com/", "cron", id.New(), now); err == nil {
		t.Fatal("accepted an unknown trigger")
	}
}

func TestReconcile(t *testing.T) {
	ws := id.New()
	first, second, third := id.New(), id.New(), id.New()
	a := site.Raised{Rule: site.RuleTitleMissing, URL: "https://example.com/a"}
	b := site.Raised{Rule: site.RuleH1Missing, URL: "https://example.com/b"}
	c := site.Raised{Rule: site.RuleThinContent, URL: "https://example.com/c", Evidence: []string{"12 words"}}

	created := site.Reconcile(nil, []site.Raised{a, b, c, a}, ws, first, id.New, now)
	if len(created) != 3 {
		t.Fatalf("created %d findings from three distinct ones", len(created))
	}
	byRule := func(fs []*site.Finding, r site.Rule) *site.Finding {
		for _, f := range fs {
			if f.Rule() == r {
				return f
			}
		}
		return nil
	}
	if err := byRule(created, site.RuleThinContent).SetStatus(site.FindingIgnored, now); err != nil {
		t.Fatal(err)
	}

	// The second crawl no longer raises a or c, and raises b with new evidence.
	b.Evidence = []string{"still"}
	later := now.Add(24 * time.Hour)
	changed := site.Reconcile(created, []site.Raised{b}, ws, second, id.New, later)
	fa, fb, fc := byRule(created, site.RuleTitleMissing), byRule(created, site.RuleH1Missing), byRule(created, site.RuleThinContent)
	if fa.Status() != site.FindingResolved || !fa.ResolvedAt().Equal(later) {
		t.Fatalf("a = %s at %v", fa.Status(), fa.ResolvedAt())
	}
	if fb.Status() != site.FindingOpen || fb.CrawlID() != second || !slices.Equal(fb.Evidence(), []string{"still"}) || !fb.FirstSeen().Equal(now) {
		t.Fatalf("b = %s %v", fb.Status(), fb.Evidence())
	}
	if fc.Status() != site.FindingIgnored {
		t.Fatalf("an ignored finding the crawl no longer raises became %s", fc.Status())
	}
	if len(changed) != 2 {
		t.Fatalf("changed %d, want a resolved and b updated", len(changed))
	}

	// The third crawl raises a again: it reopens.
	site.Reconcile(created, []site.Raised{a, b}, ws, third, id.New, later.Add(time.Hour))
	if fa.Status() != site.FindingOpen || !fa.ResolvedAt().IsZero() || !fa.FirstSeen().Equal(now) {
		t.Fatalf("a after coming back = %s", fa.Status())
	}
	if err := fa.SetStatus(site.FindingResolved, now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("a person resolved a finding: %v", err)
	}
	back, err := site.RehydrateFinding(fb.Snapshot())
	if err != nil || back.Key() != fb.Key() || back.Severity() != site.SeverityWarning {
		t.Fatalf("rehydrate = %v", err)
	}
}

func TestFindingClaims(t *testing.T) {
	ws, crawl := id.New(), id.New()
	raised := site.Raised{Rule: site.RuleTitleMissing, URL: "https://example.com/a"}
	f := site.Reconcile(nil, []site.Raised{raised}, ws, crawl, id.New, now)[0]
	one, two := id.New(), id.New()
	if !f.Claimable(now) || f.Claim(one, now) != nil || f.ClaimedBy() != one {
		t.Fatalf("first claim: claimed by %v", f.ClaimedBy())
	}
	if err := f.Claim(one, now); err != nil {
		t.Fatalf("claiming again for the same story: %v", err)
	}
	if err := f.Claim(two, now.Add(time.Hour)); !apperror.Is(err, apperror.Conflict) || f.Claimable(now.Add(time.Hour)) {
		t.Fatalf("a second story claimed a held finding: %v", err)
	}
	if err := f.Claim(two, now.Add(site.ClaimTTL)); err != nil || f.ClaimedBy() != two {
		t.Fatalf("an expired claim is not taken over: %v", err)
	}
	if f.Release(one, now) || !f.Release(two, now) || f.ClaimedBy() != uuid.Nil {
		t.Fatalf("release: claimed by %v", f.ClaimedBy())
	}
	_ = f.Claim(one, now)
	site.Reconcile([]*site.Finding{f}, nil, ws, id.New(), id.New, now)
	if f.Status() != site.FindingResolved || f.ClaimedBy() != uuid.Nil {
		t.Fatalf("a resolved finding keeps its claim: %s %v", f.Status(), f.ClaimedBy())
	}
	if err := f.Claim(one, now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("claimed a resolved finding: %v", err)
	}
	back, err := site.RehydrateFinding(f.Snapshot())
	if err != nil || back.Snapshot().ClaimedBy != f.ClaimedBy() {
		t.Fatalf("rehydrate: %v", err)
	}
}

func TestBuildCommand(t *testing.T) {
	s, err := site.New(id.New(), site.Settings{URL: "https://example.com", Build: "  npm ci && npm run build "}, id.New(), now)
	if err != nil || s.Build() != "npm ci && npm run build" {
		t.Fatalf("build = %q, %v", s.Build(), err)
	}
	if _, err := s.Update(site.Settings{URL: "https://example.com", Build: "a\nb"}, id.New(), now); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("multi-line build: %v", err)
	}
	back, err := site.Rehydrate(s.Snapshot())
	if err != nil || back.Build() != s.Build() {
		t.Fatalf("rehydrate build: %v", err)
	}
}

func TestCompetitorAndDiff(t *testing.T) {
	ws := id.New()
	c, err := site.NewCompetitor(id.New(), ws, "", "www.Globex.io", now)
	if err != nil || c.URL() != "https://www.globex.io/" || c.Name() != "globex.io" || !c.Baseline() || !c.Due(now) {
		t.Fatalf("competitor = %+v %v", c, err)
	}
	if _, err := site.NewCompetitor(id.New(), ws, "x", "ftp://nope", now); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("bad address accepted: %v", err)
	}
	pages, changes := site.DiffPages(nil, []string{"https://globex.io/a", "https://globex.io/b", "https://globex.io/a"}, true, ws, c.ID(), id.New, now)
	if len(pages) != 2 || len(changes) != 0 {
		t.Fatalf("baseline = %d pages, %d changes", len(pages), len(changes))
	}
	c.Checked(2, now)
	if c.Baseline() || c.Due(now.Add(time.Hour)) || !c.Due(now.Add(site.CompetitorEvery)) {
		t.Fatal("after the first check")
	}
	later := now.Add(site.CompetitorEvery)
	pages, changes = site.DiffPages(pages, []string{"https://globex.io/b", "https://globex.io/c"}, false, ws, c.ID(), id.New, later)
	kinds := map[string]site.ChangeKind{}
	for _, ch := range changes {
		kinds[ch.URL] = ch.Kind
	}
	if len(pages) != 3 || len(changes) != 2 || kinds["https://globex.io/c"] != site.PageAdded || kinds["https://globex.io/a"] != site.PageRemoved {
		t.Fatalf("diff = %d pages, %+v", len(pages), changes)
	}
	// A removed page that comes back counts as added again.
	pages, changes = site.DiffPages(pages, []string{"https://globex.io/a", "https://globex.io/b", "https://globex.io/c"}, false, ws, c.ID(), id.New, later.Add(time.Hour))
	if len(changes) != 1 || changes[0].Kind != site.PageAdded || changes[0].URL != "https://globex.io/a" {
		t.Fatalf("comeback = %+v", changes)
	}
	// Once removed, a page stays removed without another change.
	pages, changes = site.DiffPages(pages, []string{"https://globex.io/b", "https://globex.io/c"}, false, ws, c.ID(), id.New, later.Add(2*time.Hour))
	if len(changes) != 1 || changes[0].Kind != site.PageRemoved {
		t.Fatalf("removal = %+v", changes)
	}
	if _, changes = site.DiffPages(pages, []string{"https://globex.io/b", "https://globex.io/c"}, false, ws, c.ID(), id.New, later.Add(3*time.Hour)); len(changes) != 0 {
		t.Fatalf("removed twice: %+v", changes)
	}
	back, err := site.RehydrateCompetitor(c.Snapshot())
	if err != nil || back.Pages() != 2 {
		t.Fatalf("rehydrate = %v", err)
	}
}
