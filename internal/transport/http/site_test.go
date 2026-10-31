package httptransport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	sitesvc "github.com/ahmethakanbesel/ekin/internal/service/site/v1"
)

// runCrawls runs the queued crawl jobs the way a worker would.
func (s *server) runCrawls(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for range 5 {
		j, err := s.jobs.Claim(ctx, jobdomain.Queues, []string{sitesvc.KindCrawl}, "test", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			return
		}
		var p sitesvc.CrawlPayload
		_ = json.Unmarshal(j.Payload(), &p)
		if err := s.site.RunCrawl(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := s.jobs.Complete(ctx, j.ID(), "test", nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSiteAPI(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`<html lang="en"><head><title>Home</title><meta name="description" content="Acme."><link rel="canonical" href="/"></head>
<body><h1>Acme</h1><a href="/about">About</a><a href="/gone">Gone</a></body></html>`))
		case "/about":
			_, _ = w.Write([]byte(`<html lang="en"><head><link rel="canonical" href="/about"></head><body><h1>About</h1><a href="/">Home</a></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()

	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String() + "/site"

	var o gen.SiteOverview
	owner.call("GET", base, nil, 200, &o)
	if o.Site != nil || len(o.Open) != 0 {
		t.Fatalf("before setup = %+v", o)
	}
	owner.fail("PUT", base, map[string]any{"url": "not a url"}, 400, "ValidationFailed")
	owner.fail("POST", base+"/crawls", nil, 400, "ValidationFailed")

	owner.call("PUT", base, map[string]any{"url": site.URL, "maxPages": 20, "schedule": "daily"}, 200, &o)
	if o.Site == nil || o.Site.Url != site.URL+"/" || o.Site.Schedule != gen.SiteScheduleDaily || o.LatestCrawl == nil ||
		o.LatestCrawl.Status != gen.SiteCrawlStatusQueued || o.LatestCrawl.Trigger != gen.SiteCrawlTriggerSetup {
		t.Fatalf("after setup = %+v", o)
	}
	owner.fail("POST", base+"/crawls", nil, 409, "Conflict")

	srv.runCrawls(t)
	owner.call("GET", base, nil, 200, &o)
	if o.LastDoneCrawl == nil || o.LastDoneCrawl.Pages != 3 || o.LastDoneCrawl.Facts == nil || o.LastDoneCrawl.Facts.RobotsFound {
		t.Fatalf("after the crawl = %+v", o.LastDoneCrawl)
	}
	rules := map[string]int{}
	for _, rc := range o.Open {
		rules[rc.Rule] = rc.Count
	}
	for _, want := range []string{"broken_links", "title_missing", "description_missing", "sitemap_missing", "llms_txt_missing"} {
		if rules[want] != 1 {
			t.Errorf("open %s = %d (%v)", want, rules[want], o.Open)
		}
	}

	var pages gen.ListSitePages200JSONResponse
	owner.call("GET", base+"/pages?limit=2", nil, 200, &pages)
	if len(pages.Pages) != 2 || pages.NextCursor == nil || pages.Pages[0].Url != site.URL+"/" || pages.Pages[0].Links != 2 {
		t.Fatalf("pages = %+v", pages)
	}
	owner.call("GET", base+"/pages?class=error", nil, 200, &pages)
	if len(pages.Pages) != 1 || pages.Pages[0].Status != 404 || pages.Pages[0].Class != gen.SitePageClassError {
		t.Fatalf("error pages = %+v", pages)
	}
	owner.fail("GET", base+"/pages?class=bad", nil, 400, "ValidationFailed")

	var findings gen.ListSiteFindings200JSONResponse
	owner.call("GET", base+"/findings?status=open&url="+url.QueryEscape(site.URL+"/about"), nil, 200, &findings)
	var titleMissing *gen.SiteFinding
	for _, f := range findings.Findings {
		if f.Rule == "title_missing" {
			titleMissing = &f // a copy: findings is decoded into again below
		}
	}
	if titleMissing == nil || titleMissing.Severity != gen.SiteSeverityError {
		t.Fatalf("about's findings = %+v", findings)
	}
	owner.call("GET", base+"/findings?severity=error&status=open", nil, 200, &findings)
	if !slices.ContainsFunc(findings.Findings, func(f gen.SiteFinding) bool { return f.Rule == "broken_links" && len(f.Evidence) == 1 }) {
		t.Fatalf("errors = %+v", findings)
	}

	var updated gen.SiteFinding
	owner.call("PATCH", base+"/findings/"+titleMissing.Id.String(), map[string]any{"status": "ignored"}, 200, &updated)
	if updated.Status != gen.SiteFindingStatusIgnored {
		t.Fatalf("updated = %+v", updated)
	}
	owner.fail("PATCH", base+"/findings/"+titleMissing.Id.String(), map[string]any{"status": "resolved"}, 400, "ValidationFailed")

	var crawls gen.ListSiteCrawls200JSONResponse
	owner.call("POST", base+"/crawls", nil, 200, nil)
	owner.call("GET", base+"/crawls", nil, 200, &crawls)
	if len(crawls.Crawls) != 2 || crawls.Crawls[0].Trigger != gen.SiteCrawlTriggerManual {
		t.Fatalf("crawls = %+v", crawls)
	}

	// Changes: a build command on the site, and a change claiming a finding.
	owner.call("PUT", base, map[string]any{"url": site.URL, "maxPages": 20, "schedule": "daily", "build": "npm run build"}, 200, &o)
	if o.Site.Build == nil || *o.Site.Build != "npm run build" {
		t.Fatalf("build = %v", o.Site.Build)
	}
	srv.runCrawls(t)
	owner.call("GET", base+"/findings?status=open&rule=broken_links", nil, 200, &findings)
	if len(findings.Findings) != 1 {
		t.Fatalf("broken links = %+v", findings)
	}
	broken := findings.Findings[0]
	changes := "/api/v1/workspaces/" + ws.Id.String() + "/changes"
	owner.fail("POST", changes, map[string]any{}, 400, "ValidationFailed")
	var story gen.Story
	owner.call("POST", changes, map[string]any{"findingIds": []string{broken.Id.String()}, "instructions": "Keep the old URLs working."}, 201, &story)
	if story.Title != "Fix broken internal links" || story.Findings == nil || len(*story.Findings) != 1 ||
		len(story.Deliverables) != 1 || story.Deliverables[0].Kind != gen.DeliverableKindChange {
		t.Fatalf("change story = %+v", story)
	}
	owner.call("GET", base+"/findings?status=open&rule=broken_links", nil, 200, &findings)
	if c := findings.Findings[0].ClaimedBy; c == nil || *c != story.Id {
		t.Fatalf("claimed by = %v, want %s", c, story.Id)
	}
	owner.fail("POST", changes, map[string]any{"findingIds": []string{broken.Id.String()}}, 409, "Conflict")
}
