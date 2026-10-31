package crawl

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SitemapResult is what a site's sitemaps list.
type SitemapResult struct {
	Pages []string // page URLs on the site, normalized
	Files []string // sitemap files read
}

// Sitemap reads a site's sitemaps (those robots.txt names, else
// /sitemap.xml and /sitemap_index.xml) as EkinBot, pausing delay (or the
// site's longer Crawl-delay) between files, without fetching any page.
// It is how a competitor's pages are listed.
func (c *Crawler) Sitemap(ctx context.Context, site string, delay time.Duration) (SitemapResult, error) {
	root, origin, wait, rob, err := c.open(ctx, site, delay)
	if err != nil {
		return SitemapResult{}, err
	}
	sitemaps := rob.Sitemaps()
	if len(sitemaps) == 0 {
		sitemaps = []string{origin.String() + "sitemap.xml", origin.String() + "sitemap_index.xml"}
	}
	pages, files, err := c.sitemaps(ctx, root, sitemaps, wait)
	if err != nil {
		return SitemapResult{}, err
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range pages {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return SitemapResult{Pages: out, Files: files}, nil
}

// Titles fetches the <title> of each of urls on site that robots.txt
// lets EkinBot read, pausing between them, and returns those it found.
func (c *Crawler) Titles(ctx context.Context, site string, urls []string, delay time.Duration) (map[string]string, error) {
	root, _, wait, rob, err := c.open(ctx, site, delay)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil || !sameSite(root, u) || !rob.Allowed(agentToken, requestPath(u)) {
			continue
		}
		if err := wait(); err != nil {
			return out, err
		}
		r, err := c.get(ctx, raw, func(h http.Header) bool { return mediaType(h) == "text/html" }, maxPageBytes)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			continue
		}
		if r.status != http.StatusOK || len(r.body) == 0 {
			continue
		}
		if t := strings.TrimSpace(extract(r.body, u).title); t != "" {
			out[raw] = t
		}
	}
	return out, nil
}

// open reads site's robots.txt and returns its root and origin and a
// wait that paces requests.
func (c *Crawler) open(ctx context.Context, site string, delay time.Duration) (*url.URL, *url.URL, func() error, *robotsFile, error) {
	root, err := url.Parse(site)
	if err != nil || (root.Scheme != "http" && root.Scheme != "https") || root.Host == "" {
		return nil, nil, nil, nil, fmt.Errorf("crawl: invalid site URL %q", site)
	}
	origin := &url.URL{Scheme: root.Scheme, Host: root.Host, Path: "/"}
	rob, err := c.robots(ctx, origin)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%w: %w", ErrStart, err)
	}
	if d, ok := rob.CrawlDelay(agentToken); ok && d > delay {
		delay = min(d, maxRobotDelay)
	}
	wait := func() error {
		if delay <= 0 {
			return ctx.Err()
		}
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}
	return root, origin, wait, rob, nil
}
