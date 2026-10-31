package site_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db/dbtest"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	workspacedomain "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	siterepo "github.com/ahmethakanbesel/ekin/internal/repository/site/v1"
	workspacerepo "github.com/ahmethakanbesel/ekin/internal/repository/workspace/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

var now = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

// impls returns each Repository with two workspaces.
func impls() map[string]func(t *testing.T) (siterepo.Repository, uuid.UUID, uuid.UUID) {
	return map[string]func(t *testing.T) (siterepo.Repository, uuid.UUID, uuid.UUID){
		"inmemory": func(*testing.T) (siterepo.Repository, uuid.UUID, uuid.UUID) {
			return siterepo.NewInMemory(), id.New(), id.New()
		},
		"sqlite": func(t *testing.T) (siterepo.Repository, uuid.UUID, uuid.UUID) {
			_, tx := dbtest.Open(t)
			var ids []uuid.UUID
			for _, name := range []string{"a", "b"} {
				w, _ := workspacedomain.New(id.New(), name, time.UTC, now)
				if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, w.ID())
			}
			return siterepo.NewSQLite(tx), ids[0], ids[1]
		},
	}
}

func TestSitesAndCrawls(t *testing.T) {
	for name, open := range impls() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws, other := open(t)
			if _, err := repo.Site(ctx, ws); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("no site = %v", err)
			}
			s, _ := sitedomain.New(ws, sitedomain.Settings{URL: "https://example.com", Schedule: sitedomain.ScheduleDaily, Build: "npm run build"}, id.New(), now)
			off, _ := sitedomain.New(other, sitedomain.Settings{URL: "https://other.example", Schedule: sitedomain.ScheduleOff}, id.New(), now)
			for _, x := range []*sitedomain.Site{s, off} {
				if err := repo.PutSite(ctx, x); err != nil {
					t.Fatal(err)
				}
			}
			got, err := repo.Site(ctx, ws)
			if err != nil || got.URL() != "https://example.com/" || got.Schedule() != sitedomain.ScheduleDaily || !got.NextCrawlAt().Equal(s.NextCrawlAt()) ||
				got.Build() != "npm run build" {
				t.Fatalf("site = %+v %v", got, err)
			}
			if due, _ := repo.DueSites(ctx, now.Add(time.Hour)); len(due) != 0 {
				t.Fatalf("due too early: %d", len(due))
			}
			if due, _ := repo.DueSites(ctx, now.Add(25*time.Hour)); len(due) != 1 || due[0].WorkspaceID() != ws {
				t.Fatalf("due = %v", due)
			}

			first, _ := sitedomain.NewCrawl(id.New(), ws, s.URL(), sitedomain.TriggerSetup, id.New(), now)
			second, _ := sitedomain.NewCrawl(id.New(), ws, s.URL(), sitedomain.TriggerSchedule, uuid.Nil, now.Add(time.Hour))
			for _, c := range []*sitedomain.Crawl{first, second} {
				if err := repo.CreateCrawl(ctx, c); err != nil {
					t.Fatal(err)
				}
			}
			_ = first.Start(now)
			_ = first.Finish(sitedomain.Facts{RobotsFound: true, AIBlocked: []string{"GPTBot"}, Truncated: true}, 12, now.Add(time.Minute))
			if err := repo.SaveCrawl(ctx, first); err != nil {
				t.Fatal(err)
			}
			back, err := repo.Crawl(ctx, ws, first.ID())
			if err != nil || back.Status() != sitedomain.CrawlDone || back.Pages() != 12 || !back.Facts().Truncated ||
				!slices.Equal(back.Facts().AIBlocked, []string{"GPTBot"}) || !back.FinishedAt().Equal(now.Add(time.Minute)) {
				t.Fatalf("crawl = %+v %v", back, err)
			}
			if _, err := repo.Crawl(ctx, other, first.ID()); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("another workspace read the crawl: %v", err)
			}
			list, err := repo.Crawls(ctx, ws, 10)
			if err != nil || len(list) != 2 || list[0].ID() != second.ID() || list[1].RequestedBy() == uuid.Nil || list[0].RequestedBy() != uuid.Nil {
				t.Fatalf("crawls = %v %v", list, err)
			}
		})
	}
}

func page(path string, status int, title string) sitedomain.Page {
	return sitedomain.Page{URL: "https://example.com/" + path, Status: status, ContentType: "text/html", Title: title,
		H1: []string{title}, Links: []string{"https://example.com/"}, Depth: 1, FirstSeen: now, LastSeen: now}
}

func TestPagesAndFindings(t *testing.T) {
	for name, open := range impls() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws, other := open(t)
			pages := []sitedomain.Page{page("", 200, "Home"), page("about", 200, "Über uns"), page("gone", 404, ""), page("old", 301, "")}
			pages[3].RedirectTo = "https://example.com/about"
			if err := repo.ReplacePages(ctx, ws, pages); err != nil {
				t.Fatal(err)
			}
			if err := repo.ReplacePages(ctx, other, pages[:1]); err != nil {
				t.Fatal(err)
			}
			raised := []sitedomain.Raised{
				{Rule: sitedomain.RuleTitleMissing, URL: "https://example.com/about"},
				{Rule: sitedomain.RuleThinContent, URL: "https://example.com/about", Evidence: []string{"10 words"}},
				{Rule: sitedomain.RulePageError, URL: "https://example.com/gone", Evidence: []string{"HTTP 404"}},
				{Rule: sitedomain.RuleLLMsTxtMissing, URL: "https://example.com/"},
			}
			crawl := id.New()
			fs := sitedomain.Reconcile(nil, raised, ws, crawl, id.New, now)
			_ = fs[1].SetStatus(sitedomain.FindingIgnored, now) // thin content
			story := id.New()
			_ = fs[0].Claim(story, now)
			if err := repo.PutFindings(ctx, fs); err != nil {
				t.Fatal(err)
			}
			if got, err := repo.Finding(ctx, ws, fs[0].ID()); err != nil || got.ClaimedBy() != story || !got.ClaimedAt().Equal(now) {
				t.Fatalf("claimed finding = %+v %v", got, err)
			}
			if at, _, _ := repo.Pages(ctx, ws, siterepo.PageFilter{URLs: []string{"https://example.com/gone", "https://example.com/x"}}); len(at) != 1 || at[0].Status != 404 {
				t.Fatalf("pages by address = %v", at)
			}

			rows, next, err := repo.Pages(ctx, ws, siterepo.PageFilter{Limit: 2})
			if err != nil || len(rows) != 2 || next == "" || rows[0].URL != "https://example.com/" || rows[1].URL != "https://example.com/about" {
				t.Fatalf("first page = %v %q %v", rows, next, err)
			}
			if rows[1].OpenFindings != 1 || rows[1].Title != "Über uns" || !slices.Equal(rows[1].H1, []string{"Über uns"}) || rows[1].Depth != 1 {
				t.Fatalf("about = %+v", rows[1])
			}
			rest, next, err := repo.Pages(ctx, ws, siterepo.PageFilter{Limit: 2, After: next})
			if err != nil || len(rest) != 2 || next != "" || rest[1].RedirectTo != "https://example.com/about" {
				t.Fatalf("second page = %v %q %v", rest, next, err)
			}
			if errs, _, _ := repo.Pages(ctx, ws, siterepo.PageFilter{Class: sitedomain.ClassError}); len(errs) != 1 || errs[0].Status != 404 {
				t.Fatalf("errors = %v", errs)
			}
			if found, _, _ := repo.Pages(ctx, ws, siterepo.PageFilter{Query: "UBER"}); len(found) != 1 {
				t.Fatalf("folded search = %v", found)
			}
			seen, err := repo.FirstSeen(ctx, ws)
			if err != nil || len(seen) != 4 || !seen["https://example.com/gone"].Equal(now) {
				t.Fatalf("first seen = %v %v", seen, err)
			}

			open := []sitedomain.FindingStatus{sitedomain.FindingOpen}
			list, next, err := repo.Findings(ctx, ws, siterepo.FindingFilter{Statuses: open, Limit: 2})
			if err != nil || len(list) != 2 || next == "" || list[0].Rule() != sitedomain.RulePageError || list[1].Rule() != sitedomain.RuleTitleMissing {
				t.Fatalf("findings = %v %q %v", list, next, err)
			}
			tail, next, err := repo.Findings(ctx, ws, siterepo.FindingFilter{Statuses: open, Limit: 2, After: next})
			if err != nil || len(tail) != 1 || next != "" || tail[0].Rule() != sitedomain.RuleLLMsTxtMissing {
				t.Fatalf("findings tail = %v %q %v", tail, next, err)
			}
			if bySev, _, _ := repo.Findings(ctx, ws, siterepo.FindingFilter{Severity: sitedomain.SeverityNotice}); len(bySev) != 2 {
				t.Fatalf("notices = %v", bySev)
			}
			if byURL, _, _ := repo.Findings(ctx, ws, siterepo.FindingFilter{URL: "https://example.com/about"}); len(byURL) != 2 {
				t.Fatalf("about's findings = %v", byURL)
			}
			counts, err := repo.RuleCounts(ctx, ws, sitedomain.FindingOpen)
			if err != nil || len(counts) != 3 || counts[0].Rule != sitedomain.RulePageError || counts[0].Count != 1 {
				t.Fatalf("counts = %v %v", counts, err)
			}
			got, err := repo.Finding(ctx, ws, fs[2].ID())
			if err != nil || !slices.Equal(got.Evidence(), []string{"HTTP 404"}) || got.CrawlID() != crawl {
				t.Fatalf("finding = %+v %v", got, err)
			}
			if _, err := repo.Finding(ctx, other, fs[2].ID()); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("another workspace read the finding: %v", err)
			}

			// The next crawl resolves everything but the 404.
			all, _ := repo.AllFindings(ctx, ws)
			changed := sitedomain.Reconcile(all, raised[2:3], ws, id.New(), id.New, now.Add(time.Hour))
			if err := repo.PutFindings(ctx, changed); err != nil {
				t.Fatal(err)
			}
			if c, _ := repo.RuleCounts(ctx, ws, sitedomain.FindingResolved); len(c) != 2 {
				t.Fatalf("resolved counts = %v", c)
			}
			if c, _ := repo.RuleCounts(ctx, ws, sitedomain.FindingIgnored); len(c) != 1 || c[0].Rule != sitedomain.RuleThinContent {
				t.Fatalf("ignored counts = %v", c)
			}

			if err := repo.ClearInventory(ctx, ws); err != nil {
				t.Fatal(err)
			}
			if rows, _, _ := repo.Pages(ctx, ws, siterepo.PageFilter{}); len(rows) != 0 {
				t.Fatalf("pages after clearing = %d", len(rows))
			}
			if all, _ := repo.AllFindings(ctx, ws); len(all) != 0 {
				t.Fatalf("findings after clearing = %d", len(all))
			}
			if rows, _, _ := repo.Pages(ctx, other, siterepo.PageFilter{}); len(rows) != 1 {
				t.Fatal("clearing one workspace touched another")
			}
		})
	}
}
