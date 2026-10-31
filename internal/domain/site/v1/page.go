package site

import (
	"net/http"
	"time"
)

// Page is one URL of the latest crawl: what it answered and what its
// HTML says about itself.
type Page struct {
	URL         string
	Status      int    // 0 when the fetch failed
	Error       string // why the fetch failed
	RedirectTo  string
	ContentType string
	Title       string
	Description string
	Canonical   string
	Lang        string
	H1          []string
	Noindex     bool
	Words       int
	Types       []string // structured data (JSON-LD) types
	Links       []string // internal page links
	InSitemap   bool
	Depth       int // link hops from the start URL; -1 when links do not reach it
	FirstSeen   time.Time
	LastSeen    time.Time
}

// HTML reports whether the page answered 200 with an HTML document.
func (p Page) HTML() bool {
	return p.Status == http.StatusOK && (p.ContentType == "text/html" || p.ContentType == "application/xhtml+xml")
}

// Broken reports whether the URL answered an error or nothing.
func (p Page) Broken() bool { return p.Status >= 400 || p.Status == 0 }

// Redirect reports whether the URL redirects.
func (p Page) Redirect() bool { return p.Status >= 300 && p.Status < 400 }

// Indexable reports whether search engines are meant to index the page:
// an HTML page, not noindex, and its own canonical (or none).
func (p Page) Indexable() bool {
	return p.HTML() && !p.Noindex && (p.Canonical == "" || p.Canonical == p.URL)
}

// StatusClass groups pages for listing.
type StatusClass string

const (
	ClassOK       StatusClass = "ok"
	ClassRedirect StatusClass = "redirect"
	ClassError    StatusClass = "error"
)

// Class is the page's StatusClass.
func (p Page) Class() StatusClass {
	switch {
	case p.Broken():
		return ClassError
	case p.Redirect():
		return ClassRedirect
	}
	return ClassOK
}
