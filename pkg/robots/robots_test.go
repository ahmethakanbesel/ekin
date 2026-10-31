package robots_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/pkg/robots"
)

const sample = `# Example
User-agent: *
Disallow: /admin/
Disallow: /search
Allow: /admin/public/
Crawl-delay: 2

User-agent: GPTBot
User-agent: ClaudeBot
Disallow: /

user-agent: EkinBot/1.0
disallow: /private*.html$
allow: /private-ok.html
disallow:

Sitemap: https://example.com/sitemap.xml
sitemap: https://example.com/news.xml
`

func TestAllowed(t *testing.T) {
	r := robots.Parse(strings.NewReader(sample))
	tests := []struct {
		agent, path string
		want        bool
	}{
		{"Googlebot", "/", true},
		{"Googlebot", "/admin/settings", false},
		{"Googlebot", "/admin/public/logo.png", true}, // the longer allow wins
		{"Googlebot", "/search?q=x", false},
		{"Googlebot", "/searching", false}, // a pattern matches a prefix
		{"Googlebot", "/robots.txt", true},
		{"GPTBot", "/", false},
		{"gptbot", "/blog/post", false}, // agents match ignoring case
		{"ClaudeBot/1.0", "/blog/post", false},
		{"EkinBot", "/admin/settings", true}, // its own group replaces *
		{"EkinBot", "/private-page.html", false},
		{"EkinBot", "/private-page.html?x=1", true}, // $ anchors the end
		{"EkinBot", "/private-ok.html", true},       // equal length: allow wins
		{"PerplexityBot", "/blog", true},
	}
	for _, tt := range tests {
		if got := r.Allowed(tt.agent, tt.path); got != tt.want {
			t.Errorf("Allowed(%q, %q) = %v, want %v", tt.agent, tt.path, got, tt.want)
		}
	}
}

func TestSitemapsAndDelay(t *testing.T) {
	r := robots.Parse(strings.NewReader(sample))
	if !slices.Equal(r.Sitemaps, []string{"https://example.com/sitemap.xml", "https://example.com/news.xml"}) {
		t.Fatalf("sitemaps = %v", r.Sitemaps)
	}
	if d, ok := r.CrawlDelay("Googlebot"); !ok || d != 2*time.Second {
		t.Fatalf("delay = %v %v", d, ok)
	}
	if _, ok := r.CrawlDelay("EkinBot"); ok {
		t.Fatal("EkinBot's group sets no delay")
	}
}

func TestNoRobots(t *testing.T) {
	var r *robots.Robots
	if !r.Allowed("EkinBot", "/anything") {
		t.Fatal("a missing robots.txt must allow everything")
	}
	empty := robots.Parse(strings.NewReader("garbage\n\n: x"))
	if !empty.Allowed("EkinBot", "/x") || len(empty.Sitemaps) != 0 {
		t.Fatal("an unreadable robots.txt must allow everything")
	}
}

func TestWildcards(t *testing.T) {
	r := robots.Parse(strings.NewReader("User-agent: *\nDisallow: /*.pdf$\nDisallow: /tmp/*/cache\n"))
	for path, want := range map[string]bool{
		"/files/a.pdf":       false,
		"/files/a.pdf?dl=1":  true,
		"/tmp/x/cache/y":     false,
		"/tmp/cache":         true,
		"/tmp/a/b/cache.txt": false,
	} {
		if got := r.Allowed("Bot", path); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", path, got, want)
		}
	}
}
