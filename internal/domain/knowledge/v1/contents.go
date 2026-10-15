package knowledge

import (
	"strings"
	"unicode/utf8"
)

// Heading is one table-of-contents entry as a file declares it: a PDF
// bookmark, an EPUB navigation point or a Markdown heading, with the page
// it starts on. Level 1 is a top-level entry (a chapter).
type Heading struct {
	Level int
	Title string
	Page  int
}

// Section is a heading with the pages it covers: from its page to the
// page before the next heading at its level or above.
type Section struct {
	Level    int
	Title    string
	From, To int
}

// Contents limits.
const (
	MaxHeadings      = 2000
	MaxHeadingLevel  = 6
	maxHeadingLength = 300
)

// NormalizeHeadings keeps the usable headings of a document with
// pageCount pages: titles tidied and cut to length, entries off the
// document dropped, levels shifted so the shallowest is 1 and capped,
// and at most MaxHeadings kept.
func NormalizeHeadings(hs []Heading, pageCount int) []Heading {
	minLevel := 0
	out := make([]Heading, 0, min(len(hs), MaxHeadings))
	for _, h := range hs {
		h.Title = strings.Join(strings.Fields(h.Title), " ")
		if h.Title == "" || h.Page < 1 || h.Page > pageCount || h.Level < 1 {
			continue
		}
		if utf8.RuneCountInString(h.Title) > maxHeadingLength {
			h.Title = strings.TrimSpace(string([]rune(h.Title)[:maxHeadingLength-1])) + "…"
		}
		if minLevel == 0 || h.Level < minLevel {
			minLevel = h.Level
		}
		out = append(out, h)
		if len(out) == MaxHeadings {
			break
		}
	}
	for i := range out {
		out[i].Level = min(out[i].Level-minLevel+1, MaxHeadingLevel)
	}
	return out
}

// Contents turns headings into sections. A section ends on the page
// before the next heading at its level or above (never before its own
// page, since several headings can start on one page), or at the end of
// the document.
func Contents(hs []Heading, pageCount int) []Section {
	out := make([]Section, len(hs))
	for i, h := range hs {
		to := pageCount
		for _, next := range hs[i+1:] {
			if next.Level <= h.Level {
				to = max(h.Page, next.Page-1)
				break
			}
		}
		out[i] = Section{Level: h.Level, Title: h.Title, From: h.Page, To: to}
	}
	return out
}
