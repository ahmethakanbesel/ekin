// Package robots reads robots.txt as RFC 9309 describes it: groups of
// user-agent lines with allow and disallow rules, the most specific rule
// deciding (an allow wins a tie), `*` and `$` in patterns, and the
// file-wide sitemap lines. Crawl-delay, which the RFC leaves out but
// many sites set, is read too.
package robots

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

// MaxBytes is how much of a robots.txt is read; RFC 9309 asks crawlers
// to read at least 500 KiB.
const MaxBytes = 512 << 10

type rule struct {
	allow   bool
	pattern string
}

type group struct {
	agents []string // lowercased product tokens
	rules  []rule
	delay  time.Duration
	hasDel bool
}

// Robots is a parsed robots.txt. The nil *Robots, for a site without
// one, allows everything.
type Robots struct {
	groups   []group
	Sitemaps []string
}

// Parse reads a robots.txt. It never fails: lines it does not
// understand are skipped, as the RFC asks.
func Parse(r io.Reader) *Robots {
	out := &Robots{}
	sc := bufio.NewScanner(io.LimitReader(r, MaxBytes))
	sc.Buffer(make([]byte, 0, 64<<10), MaxBytes)
	var cur *group
	inAgents := false // the last line was a user-agent line
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if !inAgents {
				out.groups = append(out.groups, group{})
				cur = &out.groups[len(out.groups)-1]
			}
			cur.agents = append(cur.agents, strings.ToLower(token(value)))
			inAgents = true
		case "allow", "disallow":
			inAgents = false
			if cur == nil || value == "" {
				// An empty disallow allows everything; an empty
				// allow says nothing.
				continue
			}
			cur.rules = append(cur.rules, rule{allow: key == "allow", pattern: value})
		case "crawl-delay":
			inAgents = false
			if cur == nil {
				continue
			}
			if secs, err := strconv.ParseFloat(value, 64); err == nil && secs >= 0 {
				cur.delay, cur.hasDel = time.Duration(secs*float64(time.Second)), true
			}
		case "sitemap":
			if value != "" {
				out.Sitemaps = append(out.Sitemaps, value)
			}
		default:
			inAgents = false
		}
	}
	return out
}

// token is the product token of a user-agent value: "Googlebot/2.1"
// reads as "Googlebot".
func token(v string) string {
	if i := strings.IndexAny(v, "/ \t"); i >= 0 {
		v = v[:i]
	}
	return v
}

// groupsFor returns the groups that apply to agent: those naming it, or
// else those for "*". Several groups for one agent combine.
func (r *Robots) groupsFor(agent string) []group {
	agent = strings.ToLower(token(agent))
	var named, star []group
	for _, g := range r.groups {
		for _, a := range g.agents {
			switch a {
			case agent:
				named = append(named, g)
			case "*":
				star = append(star, g)
			default:
				continue
			}
			break
		}
	}
	if len(named) > 0 {
		return named
	}
	return star
}

// Allowed reports whether agent may fetch path, the URL's path and
// query as sent ("/a/b?c=d"). /robots.txt itself is always allowed.
func (r *Robots) Allowed(agent, path string) bool {
	if r == nil || path == "/robots.txt" {
		return true
	}
	if path == "" {
		path = "/"
	}
	best, allow := -1, true
	for _, g := range r.groupsFor(agent) {
		for _, ru := range g.rules {
			if !match(ru.pattern, path) {
				continue
			}
			n := len(ru.pattern)
			if n > best || (n == best && ru.allow) {
				best, allow = n, ru.allow
			}
		}
	}
	return allow
}

// CrawlDelay is the delay agent's group asks for between requests, and
// whether it asks for one.
func (r *Robots) CrawlDelay(agent string) (time.Duration, bool) {
	if r == nil {
		return 0, false
	}
	for _, g := range r.groupsFor(agent) {
		if g.hasDel {
			return g.delay, true
		}
	}
	return 0, false
}

// match reports whether pattern matches the start of path: `*` matches
// any run of characters and a trailing `$` anchors the end.
func match(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	if anchored {
		pattern = pattern[:len(pattern)-1]
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	rest := path[len(parts[0]):]
	if len(parts) == 1 {
		return !anchored || rest == ""
	}
	for i, p := range parts[1:] {
		last := i == len(parts)-2
		if last && anchored {
			return strings.HasSuffix(rest, p)
		}
		j := strings.Index(rest, p)
		if j < 0 {
			return false
		}
		rest = rest[j+len(p):]
	}
	return true
}
