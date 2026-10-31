package crawl_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/crawl"
)

// testSite serves a small site; hits counts requests per path.
func testSite(t *testing.T) (*httptest.Server, map[string]int) {
	t.Helper()
	hits := map[string]int{}
	var srv *httptest.Server
	htmlPage := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		if r.Header.Get("User-Agent") != crawl.UserAgent {
			t.Errorf("user agent = %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n\nUser-agent: GPTBot\nUser-agent: ClaudeBot\nDisallow: /\n\nSitemap: " + srv.URL + "/sitemap.xml\n"))
		case "/llms.txt":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("# Acme\n"))
		case "/sitemap.xml":
			_, _ = w.Write([]byte(`<?xml version="1.0"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>` + srv.URL + `/pages.xml.gz</loc></sitemap></sitemapindex>`))
		case "/pages.xml.gz":
			var b bytes.Buffer
			zw := gzip.NewWriter(&b)
			_, _ = zw.Write([]byte(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + srv.URL + `/</loc></url><url><loc>` + srv.URL + `/about</loc></url><url><loc>` + srv.URL + `/orphan</loc></url><url><loc>` + srv.URL + `/old</loc></url><url><loc>https://elsewhere.example/x</loc></url></urlset>`))
			_ = zw.Close()
			_, _ = w.Write(b.Bytes())
		case "/":
			htmlPage(w, `<!doctype html><html lang="en"><head><title> Acme  — Home </title>
<meta name="description" content="What Acme does."><link rel="canonical" href="/">
<script type="application/ld+json">{"@context":"https://schema.org","@graph":[{"@type":"Organization","name":"Acme"},{"@type":["WebSite","Thing"]}]}</script>
<style>.x{}</style></head><body><h1>Welcome to Acme</h1><p>One two three four five.</p>
<script>var notWords = "a b c d";</script>
<a href="/about">About</a><a href="/about#team">Team</a><a href="/missing">Gone</a><a href="/private/x">Private</a>
<a href="/logo.png">Logo</a><a href="mailto:hi@acme.test">Mail</a><a href="https://other.example/">Other</a><a href="#top">Top</a>
</body></html>`)
		case "/about":
			htmlPage(w, `<html><head><title>About</title><meta name="robots" content="noindex, follow"></head><body><h1>About</h1><h1>Team</h1><a href="/">Home</a></body></html>`)
		case "/orphan":
			w.Header().Set("X-Robots-Tag", "otherbot: noindex")
			htmlPage(w, `<html><head></head><body><p>No title here.</p></body></html>`)
		case "/old":
			http.Redirect(w, r, "/about", http.StatusMovedPermanently)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func byURL(pages []crawl.Page, u string) *crawl.Page {
	for i := range pages {
		if pages[i].URL == u {
			return &pages[i]
		}
	}
	return nil
}

func TestCrawl(t *testing.T) {
	srv, hits := testSite(t)
	var progress []int
	res, err := crawl.New(true).Crawl(context.Background(), srv.URL+"/", crawl.Options{MaxPages: 50, Progress: func(n int) { progress = append(progress, n) }})
	if err != nil {
		t.Fatal(err)
	}
	if !res.RobotsFound || !res.LLMsTxt || res.SitemapURLs != 4 || len(res.Sitemaps) != 2 || res.Truncated || res.Disallowed != 1 {
		t.Fatalf("result = %+v", res)
	}
	if !slices.Equal(res.AIBlocked, []string{"GPTBot", "ClaudeBot"}) {
		t.Fatalf("AI blocked = %v", res.AIBlocked)
	}
	if hits["/private/x"] != 0 || hits["/logo.png"] != 0 {
		t.Fatalf("fetched what it should not: %v", hits)
	}
	if len(progress) == 0 || progress[len(progress)-1] != len(res.Pages) {
		t.Fatalf("progress = %v for %d pages", progress, len(res.Pages))
	}

	home := byURL(res.Pages, srv.URL+"/")
	if home == nil || home.Status != 200 || home.Title != "Acme — Home" || home.Description != "What Acme does." ||
		home.Canonical != srv.URL+"/" || home.Lang != "en" || !slices.Equal(home.H1, []string{"Welcome to Acme"}) ||
		!home.InSitemap || home.Depth != 0 || home.Noindex {
		t.Fatalf("home = %+v", home)
	}
	if !slices.Equal(home.Types, []string{"Organization", "Thing", "WebSite"}) {
		t.Fatalf("types = %v", home.Types)
	}
	if !slices.Equal(home.Links, []string{srv.URL + "/about", srv.URL + "/missing", srv.URL + "/private/x"}) {
		t.Fatalf("links = %v", home.Links)
	}
	if home.Words != 3+5+8 { // heading, paragraph and eight link texts; no script or style
		t.Fatalf("words = %d", home.Words)
	}
	about := byURL(res.Pages, srv.URL+"/about")
	if about == nil || !about.Noindex || len(about.H1) != 2 || about.Depth != 1 || !about.InSitemap {
		t.Fatalf("about = %+v", about)
	}
	if missing := byURL(res.Pages, srv.URL+"/missing"); missing == nil || missing.Status != 404 || missing.InSitemap || missing.Depth != 1 {
		t.Fatalf("missing = %+v", missing)
	}
	if old := byURL(res.Pages, srv.URL+"/old"); old == nil || old.Status != 301 || old.RedirectTo != srv.URL+"/about" || old.Depth != -1 {
		t.Fatalf("old = %+v", old)
	}
	if orphan := byURL(res.Pages, srv.URL+"/orphan"); orphan == nil || orphan.Title != "" || orphan.Noindex || orphan.Depth != -1 {
		t.Fatalf("orphan = %+v (another crawler's noindex does not apply)", orphan)
	}
	if byURL(res.Pages, "https://elsewhere.example/x") != nil {
		t.Fatal("crawled another site from the sitemap")
	}
}

func TestCrawlStopsAtTheLimit(t *testing.T) {
	srv, _ := testSite(t)
	res, err := crawl.New(true).Crawl(context.Background(), srv.URL+"/", crawl.Options{MaxPages: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 2 || !res.Truncated {
		t.Fatalf("pages = %d, truncated %v", len(res.Pages), res.Truncated)
	}
}

func TestCrawlRefusesInternalAddresses(t *testing.T) {
	srv, hits := testSite(t)
	_, err := crawl.New(false).Crawl(context.Background(), srv.URL+"/", crawl.Options{MaxPages: 5})
	if !errors.Is(err, crawl.ErrPrivateAddress) || !errors.Is(err, crawl.ErrStart) {
		t.Fatalf("err = %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("reached the server: %v", hits)
	}
}

func TestCrawlFailsWhenRobotsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := crawl.New(true).Crawl(context.Background(), srv.URL, crawl.Options{MaxPages: 5}); !errors.Is(err, crawl.ErrStart) {
		t.Fatalf("err = %v", err)
	}
}

func TestCrawlWithoutRobotsOrSitemap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/a" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a href="/a">a</a></body></html>`))
	}))
	defer srv.Close()
	res, err := crawl.New(true).Crawl(context.Background(), srv.URL, crawl.Options{MaxPages: 5})
	if err != nil {
		t.Fatal(err)
	}
	if res.RobotsFound || res.LLMsTxt || len(res.Sitemaps) != 0 || len(res.AIBlocked) != 0 || len(res.Pages) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if !strings.HasSuffix(res.Pages[0].URL, "/") {
		t.Fatalf("start URL not normalized: %s", res.Pages[0].URL)
	}
}
