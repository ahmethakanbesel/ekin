// Package crawl fetches a website the way a polite search crawler does:
// robots.txt first (and obeyed), llms.txt and the sitemaps, then the
// pages one at a time with a pause between requests, following internal
// links up to a page limit. Redirects are recorded, not followed: each
// hop is a page of its own. It reports what each page says about itself
// and leaves judging that to the audit.
package crawl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/ahmethakanbesel/ekin/pkg/robots"
)

// UserAgent is how Ekin introduces itself; robots.txt groups for
// "EkinBot" apply to it.
const UserAgent = "Mozilla/5.0 (compatible; EkinBot/1.0; +https://github.com/ahmethakanbesel/ekin)"

const agentToken = "EkinBot"

// AIAgents are the AI crawlers and answer engines' fetchers whose access
// to the site the crawl checks in robots.txt.
var AIAgents = []string{
	"GPTBot", "OAI-SearchBot", "ChatGPT-User",
	"ClaudeBot", "Claude-SearchBot", "Claude-User",
	"PerplexityBot", "Perplexity-User",
	"Google-Extended",
}

const (
	maxPageBytes  = 5 << 20
	maxRobotDelay = 10 * time.Second
)

// Page is one URL the crawl fetched.
type Page struct {
	URL         string
	Status      int    // 0 when the fetch failed
	Error       string // why the fetch failed
	RedirectTo  string // normalized target of a 3xx
	ContentType string // media type, without parameters
	Title       string
	Description string
	Canonical   string // normalized
	Lang        string
	H1          []string
	Noindex     bool
	Words       int
	Types       []string // JSON-LD @type values
	Links       []string // normalized internal page links, deduplicated
	InSitemap   bool
	Depth       int // link hops from the start URL; -1 when links from it do not reach the page
}

// HTML reports whether the page answered 200 with an HTML document.
func (p Page) HTML() bool {
	return p.Status == http.StatusOK && (p.ContentType == "text/html" || p.ContentType == "application/xhtml+xml")
}

// Result is a finished crawl.
type Result struct {
	Pages       []Page
	RobotsFound bool
	Sitemaps    []string // sitemap files read
	SitemapURLs int      // page URLs the sitemaps list on this site
	AIBlocked   []string // AIAgents robots.txt keeps off the start URL
	LLMsTxt     bool
	Truncated   bool // stopped at the page limit with URLs left
	Disallowed  int  // URLs robots.txt keeps EkinBot off, not fetched
}

// Options tune one crawl.
type Options struct {
	MaxPages int
	// Delay is the pause between requests; a longer Crawl-delay in
	// robots.txt (up to 10s) wins.
	Delay time.Duration
	// Progress, when set, is told how many pages were fetched so far,
	// every so often.
	Progress func(fetched int)
}

// Crawler fetches sites.
type Crawler struct {
	client *http.Client
}

// New returns a Crawler. allowPrivate lets it fetch loopback and private
// addresses, for a site on the same network (and tests); otherwise it
// refuses them.
func New(allowPrivate bool) *Crawler {
	return &Crawler{client: newClient(20*time.Second, allowPrivate)}
}

type entry struct {
	url       *url.URL
	key       string
	depth     int
	inSitemap bool
	page      *Page // set once fetched
}

// ErrStart is a crawl whose start URL could not be fetched at all.
var ErrStart = errors.New("the start URL could not be fetched")

// Crawl fetches the site at start. It fails only when ctx ends or the
// start URL cannot be fetched at all; a page that answers an error is a
// result, not a failure.
func (c *Crawler) Crawl(ctx context.Context, start string, o Options) (Result, error) {
	root, err := url.Parse(start)
	if err != nil || (root.Scheme != "http" && root.Scheme != "https") || root.Host == "" {
		return Result{}, fmt.Errorf("crawl: invalid start URL %q", start)
	}
	if o.MaxPages < 1 {
		o.MaxPages = 1
	}
	origin := &url.URL{Scheme: root.Scheme, Host: root.Host, Path: "/"}
	var res Result

	rob, err := c.robots(ctx, origin)
	if err != nil {
		return res, fmt.Errorf("%w: %w", ErrStart, err)
	}
	res.RobotsFound = rob != nil
	for _, a := range AIAgents {
		if !rob.Allowed(a, requestPath(root)) {
			res.AIBlocked = append(res.AIBlocked, a)
		}
	}
	delay := o.Delay
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

	if err := wait(); err != nil {
		return res, err
	}
	res.LLMsTxt = c.llmsTxt(ctx, origin)

	seen := map[string]*entry{}
	var queue []*entry
	add := func(raw string, base *url.URL, depth int, fromSitemap bool) {
		key, ok := Normalize(base, raw)
		if !ok {
			return
		}
		u, _ := url.Parse(key)
		if !sameSite(root, u) || isAsset(u) {
			return
		}
		if e, ok := seen[key]; ok {
			if fromSitemap {
				e.inSitemap = true
			} else if depth >= 0 && (e.depth < 0 || depth < e.depth) {
				e.depth = depth
			}
			return
		}
		e := &entry{url: u, key: key, depth: depth, inSitemap: fromSitemap}
		seen[key] = e
		queue = append(queue, e)
	}
	add(root.String(), root, 0, false)

	sitemaps := rob.Sitemaps()
	if len(sitemaps) == 0 {
		sitemaps = []string{origin.String() + "sitemap.xml", origin.String() + "sitemap_index.xml"}
	}
	pages, files, err := c.sitemaps(ctx, root, sitemaps, wait)
	if err != nil {
		return res, err
	}
	res.Sitemaps, res.SitemapURLs = files, len(pages)
	for _, p := range pages {
		add(p, root, -1, true)
	}

	fetched := 0
	for i := 0; i < len(queue); i++ {
		e := queue[i]
		if fetched >= o.MaxPages {
			res.Truncated = true
			break
		}
		if !rob.Allowed(agentToken, requestPath(e.url)) {
			res.Disallowed++
			continue
		}
		if err := wait(); err != nil {
			return res, err
		}
		p, links := c.page(ctx, e)
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if i == 0 && p.Status == 0 {
			return res, fmt.Errorf("%w: %s", ErrStart, p.Error)
		}
		e.page = &p
		fetched++
		if o.Progress != nil && fetched%25 == 0 {
			o.Progress(fetched)
		}
		if p.RedirectTo != "" {
			add(p.RedirectTo, e.url, e.depth, false) // a redirect is not a hop
		}
		next := -1
		if e.depth >= 0 {
			next = e.depth + 1
		}
		for _, l := range links {
			add(l, e.url, next, false)
		}
	}
	for _, e := range queue {
		if e.page == nil {
			continue
		}
		e.page.InSitemap, e.page.Depth = e.inSitemap, e.depth
		res.Pages = append(res.Pages, *e.page)
	}
	if o.Progress != nil {
		o.Progress(fetched)
	}
	return res, nil
}

// robots fetches /robots.txt: nil when the site has none (4xx). A server
// error or an unreachable host fails the crawl, as the RFC asks a
// crawler to assume everything is disallowed then.
func (c *Crawler) robots(ctx context.Context, origin *url.URL) (*robotsFile, error) {
	r, err := c.get(ctx, origin.String()+"robots.txt", func(http.Header) bool { return true }, robots.MaxBytes)
	if err != nil {
		return nil, err
	}
	switch {
	case r.status == http.StatusOK:
		return &robotsFile{robots.Parse(bytes.NewReader(r.body))}, nil
	case r.status >= 500:
		return nil, fmt.Errorf("robots.txt answered %d", r.status)
	default:
		return nil, nil
	}
}

// robotsFile wraps robots.Robots so a site without one (nil) allows all.
type robotsFile struct{ r *robots.Robots }

func (f *robotsFile) Allowed(agent, path string) bool {
	if f == nil {
		return true
	}
	return f.r.Allowed(agent, path)
}

func (f *robotsFile) CrawlDelay(agent string) (time.Duration, bool) {
	if f == nil {
		return 0, false
	}
	return f.r.CrawlDelay(agent)
}

func (f *robotsFile) Sitemaps() []string {
	if f == nil {
		return nil
	}
	return f.r.Sitemaps
}

// llmsTxt reports whether /llms.txt answers 200 with some text.
func (c *Crawler) llmsTxt(ctx context.Context, origin *url.URL) bool {
	r, err := c.get(ctx, origin.String()+"llms.txt", func(h http.Header) bool {
		mt := mediaType(h)
		return mt == "" || strings.HasPrefix(mt, "text/")
	}, 64<<10)
	return err == nil && r.status == http.StatusOK && len(bytes.TrimSpace(r.body)) > 0
}

// sitemaps reads the given sitemap files and the ones they index, and
// returns the page URLs on this site and the files it read.
func (c *Crawler) sitemaps(ctx context.Context, root *url.URL, start []string, wait func() error) (pages, files []string, err error) {
	queue := slices.Clone(start)
	done := map[string]bool{}
	for len(queue) > 0 && len(files) < maxSitemapFiles && len(pages) < maxSitemapURLs {
		raw := queue[0]
		queue = queue[1:]
		key, ok := Normalize(root, raw)
		if !ok || done[key] {
			continue
		}
		done[key] = true
		u, _ := url.Parse(key)
		if !sameSite(root, u) {
			continue
		}
		if err := wait(); err != nil {
			return nil, nil, err
		}
		r, err := c.get(ctx, key, func(http.Header) bool { return true }, maxSitemapBytes)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			continue
		}
		if r.status != http.StatusOK {
			continue
		}
		ps, children := parseSitemap(r.body)
		if len(ps) == 0 && len(children) == 0 {
			continue
		}
		files = append(files, key)
		for _, p := range ps {
			if len(pages) >= maxSitemapURLs {
				break
			}
			if pk, ok := Normalize(root, p); ok {
				if pu, _ := url.Parse(pk); sameSite(root, pu) {
					pages = append(pages, pk)
				}
			}
		}
		queue = append(queue, children...)
	}
	return pages, files, nil
}

// page fetches one entry and returns what it found and the links to
// follow from it.
func (c *Crawler) page(ctx context.Context, e *entry) (Page, []string) {
	p := Page{URL: e.key}
	r, err := c.get(ctx, e.key, func(h http.Header) bool {
		mt := mediaType(h)
		return mt == "text/html" || mt == "application/xhtml+xml"
	}, maxPageBytes)
	if err != nil {
		p.Error = err.Error()
		return p, nil
	}
	p.Status, p.ContentType = r.status, mediaType(r.header)
	if r.status >= 300 && r.status < 400 && r.location != "" {
		if to, ok := Normalize(e.url, r.location); ok {
			p.RedirectTo = to
		}
	}
	for _, v := range r.header.Values("X-Robots-Tag") {
		// "otherbot: noindex" is for one crawler; a bare value for all.
		if name, _, scoped := strings.Cut(v, ":"); scoped {
			name = strings.ToLower(strings.TrimSpace(name))
			if !strings.ContainsAny(name, " ,") && name != "unavailable_after" && name != strings.ToLower(agentToken) {
				continue
			}
		}
		if hasNoindex(v) {
			p.Noindex = true
		}
	}
	if !p.HTML() || len(r.body) == 0 {
		return p, nil
	}
	f := extract(r.body, e.url)
	p.Title, p.Description, p.Lang, p.H1, p.Words, p.Types = f.title, f.description, f.lang, f.h1, f.words, f.types
	p.Noindex = p.Noindex || f.noindex
	if f.canonical != "" {
		if can, ok := Normalize(e.url, f.canonical); ok {
			p.Canonical = can
		}
	}
	links := make([]string, 0, len(f.links))
	for _, l := range f.links {
		key, ok := Normalize(e.url, l)
		if !ok {
			continue
		}
		u, _ := url.Parse(key)
		if key != e.key && sameSite(e.url, u) && !isAsset(u) {
			links = append(links, key)
		}
	}
	slices.Sort(links)
	p.Links = slices.Compact(links)
	return p, p.Links
}

func mediaType(h http.Header) string {
	mt, _, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil {
		return ""
	}
	return strings.ToLower(mt)
}
