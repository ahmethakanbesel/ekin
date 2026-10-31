package site

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Rule is one check the audit runs.
type Rule string

// The rules. Site-wide ones are raised on the site's address.
const (
	RuleRobotsBlocksAI   Rule = "robots_blocks_ai"
	RuleLLMsTxtMissing   Rule = "llms_txt_missing"
	RuleSitemapMissing   Rule = "sitemap_missing"
	RuleHomeNoStructured Rule = "home_structured_data_missing"
	RulePageError        Rule = "page_error"
	RuleBrokenLinks      Rule = "broken_links"
	RuleRedirectChain    Rule = "redirect_chain"
	RuleSitemapRedirect  Rule = "sitemap_lists_redirect"
	RuleSitemapNoindex   Rule = "sitemap_lists_noindex"
	RuleCanonicalBroken  Rule = "canonical_broken"
	RuleTitleMissing     Rule = "title_missing"
	RuleTitleLong        Rule = "title_long"
	RuleTitleDuplicate   Rule = "title_duplicate"
	RuleDescMissing      Rule = "description_missing"
	RuleDescLong         Rule = "description_long"
	RuleDescDuplicate    Rule = "description_duplicate"
	RuleH1Missing        Rule = "h1_missing"
	RuleH1Multiple       Rule = "h1_multiple"
	RuleCanonicalMissing Rule = "canonical_missing"
	RuleThinContent      Rule = "thin_content"
	RuleLangMissing      Rule = "lang_missing"
	RuleOrphan           Rule = "orphan"
	RuleNotInSitemap     Rule = "not_in_sitemap"
)

var severities = map[Rule]Severity{
	RuleRobotsBlocksAI:   SeverityWarning,
	RuleLLMsTxtMissing:   SeverityNotice,
	RuleSitemapMissing:   SeverityWarning,
	RuleHomeNoStructured: SeverityNotice,
	RulePageError:        SeverityError,
	RuleBrokenLinks:      SeverityError,
	RuleRedirectChain:    SeverityWarning,
	RuleSitemapRedirect:  SeverityWarning,
	RuleSitemapNoindex:   SeverityWarning,
	RuleCanonicalBroken:  SeverityError,
	RuleTitleMissing:     SeverityError,
	RuleTitleLong:        SeverityNotice,
	RuleTitleDuplicate:   SeverityWarning,
	RuleDescMissing:      SeverityWarning,
	RuleDescLong:         SeverityNotice,
	RuleDescDuplicate:    SeverityNotice,
	RuleH1Missing:        SeverityWarning,
	RuleH1Multiple:       SeverityNotice,
	RuleCanonicalMissing: SeverityNotice,
	RuleThinContent:      SeverityNotice,
	RuleLangMissing:      SeverityNotice,
	RuleOrphan:           SeverityNotice,
	RuleNotInSitemap:     SeverityNotice,
}

var fixes = map[Rule]string{
	RuleRobotsBlocksAI:   "Let AI crawlers read the site in robots.txt",
	RuleLLMsTxtMissing:   "Add an llms.txt",
	RuleSitemapMissing:   "Add a sitemap",
	RuleHomeNoStructured: "Add structured data to the home page",
	RulePageError:        "Fix pages that answer with an error",
	RuleBrokenLinks:      "Fix broken internal links",
	RuleRedirectChain:    "Shorten redirect chains",
	RuleSitemapRedirect:  "Drop redirects from the sitemap",
	RuleSitemapNoindex:   "Drop noindex pages from the sitemap",
	RuleCanonicalBroken:  "Fix canonical links that point at broken pages",
	RuleTitleMissing:     "Add missing page titles",
	RuleTitleLong:        "Shorten long page titles",
	RuleTitleDuplicate:   "Make duplicate page titles unique",
	RuleDescMissing:      "Add missing meta descriptions",
	RuleDescLong:         "Shorten long meta descriptions",
	RuleDescDuplicate:    "Make duplicate meta descriptions unique",
	RuleH1Missing:        "Add a main heading to pages without one",
	RuleH1Multiple:       "Keep one main heading per page",
	RuleCanonicalMissing: "Add canonical links",
	RuleThinContent:      "Expand thin pages",
	RuleLangMissing:      "Declare the page language",
	RuleOrphan:           "Link to orphan pages",
	RuleNotInSitemap:     "Add pages missing from the sitemap",
}

// Fix is what fixing r means, as a story title would say it.
func (r Rule) Fix() string {
	if f, ok := fixes[r]; ok {
		return f
	}
	return "Fix " + strings.ReplaceAll(string(r), "_", " ")
}

// Severity is how much breaking r matters; a rule this version does not
// know reads as a notice.
func (r Rule) Severity() Severity {
	if s, ok := severities[r]; ok {
		return s
	}
	return SeverityNotice
}

// Rules lists the rules the audit runs.
func Rules() []Rule {
	out := make([]Rule, 0, len(severities))
	for r := range severities {
		out = append(out, r)
	}
	slices.Sort(out)
	return out
}

// Thresholds the audit applies.
const (
	TitleMaxRunes       = 60
	DescriptionMaxRunes = 160
	ThinWords           = 200
	maxRedirectHops     = 10
	evidenceURLs        = 10
)

// Inventory is what the audit reads: the site's address, the latest
// crawl's pages and what it learnt about the site as a whole.
type Inventory struct {
	Root  string
	Pages []Page
	Facts Facts
}

// Audit runs every rule over the inventory and returns what it raises,
// in a stable order. It decides nothing a person could not check from
// the evidence.
func Audit(inv Inventory) []Raised {
	a := auditor{inv: inv, byURL: make(map[string]*Page, len(inv.Pages)), linkedFrom: map[string][]string{}}
	for i := range inv.Pages {
		p := &inv.Pages[i]
		a.byURL[p.URL] = p
	}
	for _, p := range inv.Pages {
		if !p.HTML() {
			continue
		}
		for _, l := range p.Links {
			a.linkedFrom[l] = append(a.linkedFrom[l], p.URL)
		}
	}
	a.siteWide()
	a.statuses()
	a.content()
	a.discovery()
	slices.SortStableFunc(a.out, func(x, y Raised) int {
		if c := x.Rule.Severity().Rank() - y.Rule.Severity().Rank(); c != 0 {
			return c
		}
		return strings.Compare(x.Key(), y.Key())
	})
	return a.out
}

type auditor struct {
	inv        Inventory
	byURL      map[string]*Page
	linkedFrom map[string][]string // target → linking pages
	out        []Raised
}

func (a *auditor) raise(r Rule, url string, evidence ...string) {
	a.out = append(a.out, Raised{Rule: r, URL: url, Evidence: evidence})
}

func (a *auditor) siteWide() {
	f, root := a.inv.Facts, a.inv.Root
	if len(f.AIBlocked) > 0 {
		a.raise(RuleRobotsBlocksAI, root, f.AIBlocked...)
	}
	if !f.LLMsTxt {
		a.raise(RuleLLMsTxtMissing, root)
	}
	if len(f.Sitemaps) == 0 {
		a.raise(RuleSitemapMissing, root)
	}
	if home, ok := a.byURL[root]; ok && home.HTML() && len(home.Types) == 0 {
		a.raise(RuleHomeNoStructured, root)
	}
}

// statuses covers what URLs answer: errors, redirects and canonicals.
func (a *auditor) statuses() {
	for _, p := range a.inv.Pages {
		switch {
		case p.Broken():
			if p.InSitemap || p.URL == a.inv.Root {
				ev := []string{describeStatus(p)}
				if p.InSitemap {
					ev = append(ev, "listed in the sitemap")
				}
				a.raise(RulePageError, p.URL, ev...)
			}
		case p.Redirect():
			if chain := a.chain(p); len(chain) > 2 {
				a.raise(RuleRedirectChain, p.URL, chain...)
			}
			if p.InSitemap {
				a.raise(RuleSitemapRedirect, p.URL, "redirects to "+p.RedirectTo)
			}
		case p.HTML():
			if p.InSitemap && p.Noindex {
				a.raise(RuleSitemapNoindex, p.URL)
			}
			if p.Canonical != "" && p.Canonical != p.URL {
				if t, ok := a.byURL[p.Canonical]; ok && t.Status != 200 {
					a.raise(RuleCanonicalBroken, p.URL, p.Canonical+" — "+describeStatus(*t))
				}
			}
		}
		if p.HTML() {
			var broken []string
			for _, l := range p.Links {
				if t, ok := a.byURL[l]; ok && t.Broken() {
					broken = append(broken, l+" — "+describeStatus(*t))
				}
			}
			if len(broken) > 0 {
				a.raise(RuleBrokenLinks, p.URL, capList(broken, evidenceURLs)...)
			}
		}
	}
}

// chain follows p's redirects and returns the URLs passed through,
// starting with p; it stops at a page that does not redirect, one the
// crawl did not fetch, a loop, or after maxRedirectHops.
func (a *auditor) chain(p Page) []string {
	out := []string{p.URL}
	cur := p
	for range maxRedirectHops {
		if cur.RedirectTo == "" || slices.Contains(out, cur.RedirectTo) {
			if cur.RedirectTo != "" {
				out = append(out, cur.RedirectTo+" (loop)")
			}
			return out
		}
		out = append(out, cur.RedirectTo)
		next, ok := a.byURL[cur.RedirectTo]
		if !ok || !next.Redirect() {
			return out
		}
		cur = *next
	}
	return out
}

// content covers what indexable pages say about themselves.
func (a *auditor) content() {
	titles, descs := map[string][]string{}, map[string][]string{}
	for _, p := range a.inv.Pages {
		if !p.Indexable() {
			continue
		}
		switch n := utf8.RuneCountInString(p.Title); {
		case n == 0:
			a.raise(RuleTitleMissing, p.URL)
		case n > TitleMaxRunes:
			a.raise(RuleTitleLong, p.URL, p.Title, fmt.Sprintf("%d characters", n))
		}
		if p.Title != "" {
			titles[fold(p.Title)] = append(titles[fold(p.Title)], p.URL)
		}
		switch n := utf8.RuneCountInString(p.Description); {
		case n == 0:
			a.raise(RuleDescMissing, p.URL)
		case n > DescriptionMaxRunes:
			a.raise(RuleDescLong, p.URL, p.Description, fmt.Sprintf("%d characters", n))
		}
		if p.Description != "" {
			descs[fold(p.Description)] = append(descs[fold(p.Description)], p.URL)
		}
		if !slices.ContainsFunc(p.H1, func(h string) bool { return h != "" }) {
			a.raise(RuleH1Missing, p.URL)
		}
		if len(p.H1) > 1 {
			a.raise(RuleH1Multiple, p.URL, p.H1...)
		}
		if p.Canonical == "" {
			a.raise(RuleCanonicalMissing, p.URL)
		}
		if p.Words < ThinWords {
			a.raise(RuleThinContent, p.URL, fmt.Sprintf("%d words", p.Words))
		}
		if p.Lang == "" {
			a.raise(RuleLangMissing, p.URL)
		}
	}
	a.duplicates(RuleTitleDuplicate, titles)
	a.duplicates(RuleDescDuplicate, descs)
}

// duplicates raises rule on every page sharing a value with another,
// naming the others.
func (a *auditor) duplicates(rule Rule, groups map[string][]string) {
	for _, urls := range groups {
		if len(urls) < 2 {
			continue
		}
		for _, u := range urls {
			others := slices.DeleteFunc(slices.Clone(urls), func(o string) bool { return o == u })
			slices.Sort(others)
			a.raise(rule, u, capList(others, evidenceURLs)...)
		}
	}
}

// discovery covers how search engines find pages: sitemaps and links.
func (a *auditor) discovery() {
	f := a.inv.Facts
	for _, p := range a.inv.Pages {
		if !p.Indexable() {
			continue
		}
		// Without every page fetched, a page may be linked from one
		// that was not; orphans are only judged after a full crawl.
		if !f.Truncated && p.InSitemap && p.URL != a.inv.Root && len(a.linkedFrom[p.URL]) == 0 {
			a.raise(RuleOrphan, p.URL)
		}
		if len(f.Sitemaps) > 0 && !p.InSitemap {
			ev := []string{}
			if from := a.linkedFrom[p.URL]; len(from) > 0 {
				ev = append(ev, "linked from "+from[0])
			}
			a.raise(RuleNotInSitemap, p.URL, ev...)
		}
	}
}

func describeStatus(p Page) string {
	if p.Status == 0 {
		if p.Error == "" {
			return "no answer"
		}
		return p.Error
	}
	return fmt.Sprintf("HTTP %d", p.Status)
}

func capList(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return append(slices.Clone(items[:n]), fmt.Sprintf("and %d more", len(items)-n))
}

// fold compares titles and descriptions ignoring case and spacing.
func fold(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
