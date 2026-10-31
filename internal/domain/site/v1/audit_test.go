package site_test

import (
	"slices"
	"strings"
	"testing"

	site "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
)

const root = "https://example.com/"

// good is an indexable page that breaks no content rule.
func good(path string, links ...string) site.Page {
	u := root + path
	return site.Page{URL: u, Status: 200, ContentType: "text/html", Title: "Page " + path, Description: "About " + path,
		Canonical: u, Lang: "en", H1: []string{"Heading " + path}, Words: 400, Links: links, InSitemap: true, Depth: 1}
}

func raised(rs []site.Raised) map[string][]string {
	out := map[string][]string{}
	for _, r := range rs {
		out[r.Key()] = r.Evidence
	}
	return out
}

func TestAuditCleanSite(t *testing.T) {
	home := good("", root+"a")
	home.Types = []string{"Organization"}
	inv := site.Inventory{Root: root, Pages: []site.Page{home, good("a", root)},
		Facts: site.Facts{Sitemaps: []string{root + "sitemap.xml"}, LLMsTxt: true}}
	if got := site.Audit(inv); len(got) != 0 {
		t.Fatalf("a clean site raised %v", got)
	}
}

func TestAuditRules(t *testing.T) {
	home := good("", root+"dup1", root+"dup2", root+"missing", root+"old", root+"thin", root+"noindex", root+"long", root+"bare")
	dup1, dup2 := good("dup1"), good("dup2")
	dup1.Title, dup2.Title = "Same title", "same  TITLE"
	dup1.Description, dup2.Description = "Same description", "Same description"
	long := good("long", root)
	long.Title = strings.Repeat("x", 61)
	long.Description = strings.Repeat("y", 161)
	long.H1 = []string{"One", "Two"}
	bare := good("bare", root)
	bare.Title, bare.Description, bare.Canonical, bare.Lang, bare.H1 = "", "", "", "", []string{""}
	thin := good("thin")
	thin.Words = 50
	noindex := good("noindex")
	noindex.Noindex = true
	orphan := good("orphan")
	unlisted := good("unlisted")
	unlisted.InSitemap = false
	canon := good("canon", root+"unlisted")
	canon.Canonical = root + "missing"
	pages := []site.Page{
		home, dup1, dup2, long, bare, thin, noindex, orphan, unlisted, canon,
		{URL: root + "missing", Status: 404, ContentType: "text/html", InSitemap: true},
		{URL: root + "old", Status: 301, RedirectTo: root + "older", InSitemap: true},
		{URL: root + "older", Status: 302, RedirectTo: root + "a"},
		{URL: root + "down", Status: 0, Error: "connection reset", InSitemap: true},
	}
	inv := site.Inventory{Root: root, Pages: pages,
		Facts: site.Facts{AIBlocked: []string{"GPTBot", "ClaudeBot"}, Sitemaps: []string{root + "sitemap.xml"}}}
	got := raised(site.Audit(inv))

	want := map[string][]string{
		"robots_blocks_ai " + root:                  {"GPTBot", "ClaudeBot"},
		"llms_txt_missing " + root:                  nil,
		"home_structured_data_missing " + root:      nil,
		"page_error " + root + "missing":            {"HTTP 404", "listed in the sitemap"},
		"page_error " + root + "down":               {"connection reset", "listed in the sitemap"},
		"broken_links " + root:                      {root + "missing — HTTP 404"},
		"redirect_chain " + root + "old":            {root + "old", root + "older", root + "a"},
		"sitemap_lists_redirect " + root + "old":    {"redirects to " + root + "older"},
		"sitemap_lists_noindex " + root + "noindex": nil,
		"canonical_broken " + root + "canon":        {root + "missing — HTTP 404"},
		"title_duplicate " + root + "dup1":          {root + "dup2"},
		"title_duplicate " + root + "dup2":          {root + "dup1"},
		"description_duplicate " + root + "dup1":    {root + "dup2"},
		"description_duplicate " + root + "dup2":    {root + "dup1"},
		"title_long " + root + "long":               {strings.Repeat("x", 61), "61 characters"},
		"description_long " + root + "long":         {strings.Repeat("y", 161), "161 characters"},
		"h1_multiple " + root + "long":              {"One", "Two"},
		"title_missing " + root + "bare":            nil,
		"description_missing " + root + "bare":      nil,
		"h1_missing " + root + "bare":               nil,
		"canonical_missing " + root + "bare":        nil,
		"lang_missing " + root + "bare":             nil,
		"thin_content " + root + "thin":             {"50 words"},
		"orphan " + root + "orphan":                 nil,
		"not_in_sitemap " + root + "unlisted":       {"linked from " + root + "canon"},
	}
	for k, ev := range want {
		gotEv, ok := got[k]
		if !ok {
			t.Errorf("missing %s", k)
			continue
		}
		if len(ev) > 0 && !slices.Equal(gotEv, ev) {
			t.Errorf("%s evidence = %q, want %q", k, gotEv, ev)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected %s %q", k, got[k])
		}
	}
}

func TestAuditOrdersBySeverity(t *testing.T) {
	bare := good("bare", root)
	bare.Title, bare.Words = "", 10
	got := site.Audit(site.Inventory{Root: root, Pages: []site.Page{good("", root+"bare"), bare}, Facts: site.Facts{LLMsTxt: true}})
	for i := 1; i < len(got); i++ {
		if got[i-1].Rule.Severity().Rank() > got[i].Rule.Severity().Rank() {
			t.Fatalf("not ordered by severity: %v", got)
		}
	}
	if got[0].Rule != site.RuleTitleMissing {
		t.Fatalf("first = %s", got[0].Rule)
	}
}

func TestAuditSkipsOrphansAfterATruncatedCrawl(t *testing.T) {
	inv := site.Inventory{Root: root, Pages: []site.Page{good(""), good("orphan")},
		Facts: site.Facts{Sitemaps: []string{"x"}, LLMsTxt: true, Truncated: true}}
	for _, r := range site.Audit(inv) {
		if r.Rule == site.RuleOrphan {
			t.Fatalf("raised an orphan after a truncated crawl: %v", r)
		}
	}
}
