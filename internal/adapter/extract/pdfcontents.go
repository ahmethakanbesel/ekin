package extract

import (
	"cmp"
	"context"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	knowledgedomain "kurgu/internal/domain/knowledge/v1"
)

// pdfContents builds a PDF's table of contents from its bookmarks and
// its headings. Bookmarks often stop at chapters, so the headings set in
// a larger font inside each innermost bookmark are added under it; a PDF
// without bookmarks gets its headings alone. A missing tool or an
// unreadable file yields none.
func (e *Extractor) pdfContents(ctx context.Context, p string, pageCount int) []knowledgedomain.Heading {
	if e.PDFToHTML == "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "kurgu-outline-*")
	if err != nil {
		return nil
	}
	defer func() { _ = os.RemoveAll(dir) }()
	cmd := exec.CommandContext(ctx, e.PDFToHTML, "-xml", "-i", "-q", "-stdout", p) //nolint:gosec // fixed binary, temp path
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return nil
	}
	outline, lines := parsePDFXML(stdout)
	_, _ = io.Copy(io.Discard, stdout)
	if err := cmd.Wait(); err != nil {
		return nil
	}
	return mergeContents(knowledgedomain.NormalizeHeadings(outline, pageCount), detectHeadings(lines), pageCount)
}

// pdfLine is one <text> element of pdftohtml's XML: a line (or part of
// one) set in a single font.
type pdfLine struct {
	page, top, height, size int
	text                    string
}

// parsePDFXML reads pdftohtml's XML: the bookmarks (nested <outline>
// elements of <item page="N">title</item>, the depth being the level)
// and every text line with its font size.
func parsePDFXML(r io.Reader) (outline []knowledgedomain.Heading, lines []pdfLine) {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	var (
		depth, page int
		sizes       = map[string]int{} // font id → size
		item        *knowledgedomain.Heading
		line        *pdfLine
		text        strings.Builder
	)
	attr := func(se xml.StartElement, name string) string {
		for _, a := range se.Attr {
			if a.Name.Local == name {
				return a.Value
			}
		}
		return ""
	}
	num := func(se xml.StartElement, name string) int {
		n, _ := strconv.Atoi(attr(se, name))
		return n
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return outline, lines // io.EOF, or a broken tail: keep what was read
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "page":
				page = num(t, "number")
			case "fontspec":
				sizes[attr(t, "id")] = num(t, "size")
			case "text":
				line = &pdfLine{page: page, top: num(t, "top"), height: num(t, "height"), size: sizes[attr(t, "font")]}
				text.Reset()
			case "outline":
				depth++
			case "item":
				if depth > 0 {
					item = &knowledgedomain.Heading{Level: depth, Page: num(t, "page")}
					text.Reset()
				}
			}
		case xml.CharData:
			if line != nil || item != nil {
				text.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "text":
				if line != nil {
					line.text = text.String()
					lines = append(lines, *line)
				}
				line = nil
			case "outline":
				depth = max(0, depth-1)
			case "item":
				if item != nil && item.Page > 0 {
					item.Title = text.String()
					outline = append(outline, *item)
				}
				item = nil
			}
		}
	}
}

// heading is a detected heading: a short block in a font larger than
// the body text.
type heading struct {
	page, size, seq int // seq orders headings through the document
	title           string
}

// Heading detection limits.
const (
	headingMinStep  = 2   // points above the body size
	headingMaxLines = 3   // a longer block in a large font is not a heading
	headingMaxRunes = 150 //
	headingMaxPages = 2   // a title on more pages is a running header
)

// detectHeadings finds the blocks set larger than the body text (the
// size that sets the most characters): consecutive lines on a page in
// one size are one block. Long blocks, blocks without words and titles
// that repeat across pages are left out.
func detectHeadings(lines []pdfLine) []heading {
	chars := map[int]int{}
	for _, l := range lines {
		chars[l.size] += utf8.RuneCountInString(l.text)
	}
	body, most := 0, -1
	for size, n := range chars {
		if n > most || (n == most && size < body) {
			body, most = size, n
		}
	}

	type block struct {
		heading
		lines, bottom int
	}
	var blocks []block
	for _, l := range lines {
		if l.size < body+headingMinStep || strings.TrimSpace(l.text) == "" {
			continue
		}
		if n := len(blocks); n > 0 {
			b := &blocks[n-1]
			if b.page == l.page && b.size == l.size && l.top-b.bottom <= l.height {
				if l.top > b.bottom-l.height { // a new line, not more of the same one
					b.lines++
				}
				b.title += " " + l.text
				b.bottom = max(b.bottom, l.top+l.height)
				continue
			}
		}
		blocks = append(blocks, block{heading: heading{page: l.page, size: l.size, title: l.text}, lines: 1, bottom: l.top + l.height})
	}

	pages := map[string]map[int]bool{}
	for _, b := range blocks {
		k := titleKey(b.title)
		if pages[k] == nil {
			pages[k] = map[int]bool{}
		}
		pages[k][b.page] = true
	}
	var out []heading
	for _, b := range blocks {
		title := strings.Join(strings.Fields(b.title), " ")
		if b.lines > headingMaxLines || utf8.RuneCountInString(title) > headingMaxRunes ||
			letters(title) < 3 || len(pages[titleKey(title)]) > headingMaxPages {
			continue
		}
		out = append(out, heading{page: b.page, size: b.size, seq: len(out), title: title})
	}
	return out
}

// mergeContents nests the detected headings under the bookmarks: within
// each innermost bookmark's pages, the two largest heading sizes below
// the bookmark's own title become the next two levels. Headings before
// that title on its first page belong to the previous entry and are
// dropped, as are those repeating a bookmark. Without bookmarks the
// three largest sizes become levels 1–3.
func mergeContents(bookmarks []knowledgedomain.Heading, found []heading, pageCount int) []knowledgedomain.Heading {
	if len(bookmarks) == 0 {
		return leveled(found, 1, 3)
	}
	marked := map[string]bool{}
	for _, b := range bookmarks {
		marked[titleKey(b.Title)] = true
	}
	sections := knowledgedomain.Contents(bookmarks, pageCount)
	out := make([]knowledgedomain.Heading, 0, len(bookmarks))
	for i, b := range bookmarks {
		out = append(out, b)
		if i+1 < len(bookmarks) && bookmarks[i+1].Level > b.Level {
			continue // not innermost: its children say more than headings
		}
		var inside []heading
		limit := 0 // sizes at or above the bookmark's own title are not below it
		for _, h := range found {
			if h.page < sections[i].From || h.page > sections[i].To {
				continue
			}
			if h.page == sections[i].From && titleKey(h.title) == titleKey(b.Title) {
				inside, limit = inside[:0], h.size // what came before is the previous entry's
				continue
			}
			if !marked[titleKey(h.title)] {
				inside = append(inside, h)
			}
		}
		if limit > 0 {
			inside = slices.DeleteFunc(inside, func(h heading) bool { return h.size >= limit })
		}
		out = append(out, leveled(inside, b.Level+1, 2)...)
	}
	return out
}

// leveled turns headings into entries: the largest of their sizes is
// level first, the next one level first+1, up to depth sizes; smaller
// sizes are left out.
func leveled(hs []heading, first, depth int) []knowledgedomain.Heading {
	var sizes []int
	for _, h := range hs {
		if !slices.Contains(sizes, h.size) {
			sizes = append(sizes, h.size)
		}
	}
	slices.SortFunc(sizes, func(a, b int) int { return cmp.Compare(b, a) })
	sizes = sizes[:min(len(sizes), depth)]
	var out []knowledgedomain.Heading
	for _, h := range hs {
		if lvl := slices.Index(sizes, h.size); lvl >= 0 {
			out = append(out, knowledgedomain.Heading{Level: first + lvl, Title: h.title, Page: h.page})
		}
	}
	return out
}

// titleKey compares titles by their letters and digits, case-folded.
func titleKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func letters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}
