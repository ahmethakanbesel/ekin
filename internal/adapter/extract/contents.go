package extract

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	knowledgedomain "kurgu/internal/domain/knowledge/v1"
)

// pdfOutline reads the PDF's bookmarks. pdftohtml's XML output lists
// them after the first page as nested <outline> elements of
// <item page="N">title</item>; the nesting depth is the level. A PDF
// without bookmarks, or a missing tool, yields none.
func (e *Extractor) pdfOutline(ctx context.Context, p string) []knowledgedomain.Heading {
	if e.PDFToHTML == "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "kurgu-outline-*")
	if err != nil {
		return nil
	}
	defer func() { _ = os.RemoveAll(dir) }()
	cmd := exec.CommandContext(ctx, e.PDFToHTML, "-xml", "-i", "-q", "-stdout", "-f", "1", "-l", "1", p) //nolint:gosec // fixed binary, temp path
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parsePDFOutline(out)
}

func parsePDFOutline(doc []byte) []knowledgedomain.Heading {
	dec := xml.NewDecoder(bytes.NewReader(doc))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	var (
		out   []knowledgedomain.Heading
		depth int
		item  *knowledgedomain.Heading
		title strings.Builder
	)
	for {
		tok, err := dec.Token()
		if err != nil {
			return out // io.EOF, or a broken tail: keep what was read
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "outline":
				depth++
			case "item":
				if depth == 0 {
					continue
				}
				page := 0
				for _, a := range t.Attr {
					if a.Name.Local == "page" {
						page, _ = strconv.Atoi(a.Value)
					}
				}
				item = &knowledgedomain.Heading{Level: depth, Page: page}
				title.Reset()
			}
		case xml.CharData:
			if item != nil {
				title.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "outline":
				depth = max(0, depth-1)
			case "item":
				if item != nil && item.Page > 0 {
					item.Title = title.String()
					out = append(out, *item)
				}
				item = nil
			}
		}
	}
}

// epubContents reads the EPUB 3 navigation document, or else the EPUB 2
// NCX, and places each entry on the page its file starts on (startOf,
// by file path in the archive). Fragments within a file are not
// resolved: an entry starts where its file does.
func epubContents(pkg opf, base string, read func(string) ([]byte, error), startOf map[string]int) []knowledgedomain.Heading {
	var navPath, ncxPath string
	for _, it := range pkg.Manifest {
		if strings.Contains(" "+it.Properties+" ", " nav ") {
			navPath = resolve(base, it.Href)
		}
		if it.ID == pkg.Spine.TOC && pkg.Spine.TOC != "" {
			ncxPath = resolve(base, it.Href)
		}
	}
	type entry struct {
		level int
		title string
		href  string // resolved file path
	}
	var entries []entry
	if navPath != "" {
		if doc, err := read(navPath); err == nil {
			for _, l := range navLinks(doc) {
				entries = append(entries, entry{l.level, l.title, resolve(path.Dir(navPath), l.href)})
			}
		}
	}
	if len(entries) == 0 && ncxPath != "" {
		if doc, err := read(ncxPath); err == nil {
			var ncx struct {
				Points []navPoint `xml:"navMap>navPoint"`
			}
			if xml.Unmarshal(doc, &ncx) == nil {
				var walk func(ps []navPoint, level int)
				walk = func(ps []navPoint, level int) {
					for _, p := range ps {
						entries = append(entries, entry{level, p.Label, resolve(path.Dir(ncxPath), p.Content.Src)})
						walk(p.Children, level+1)
					}
				}
				walk(ncx.Points, 1)
			}
		}
	}
	out := make([]knowledgedomain.Heading, 0, len(entries))
	for _, e := range entries {
		if page, ok := startOf[e.href]; ok {
			out = append(out, knowledgedomain.Heading{Level: e.level, Title: e.title, Page: page})
		}
	}
	return out
}

type navPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []navPoint `xml:"navPoint"`
}

type navLink struct {
	level       int
	title, href string
}

// navLinks returns the links of the navigation document's table of
// contents (the <nav epub:type="toc">, or else the first <nav>), each at
// the depth of its list.
func navLinks(doc []byte) []navLink {
	root, err := html.Parse(bytes.NewReader(doc))
	if err != nil {
		return nil
	}
	var navs []*html.Node
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "nav" {
			navs = append(navs, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	if len(navs) == 0 {
		return nil
	}
	nav := navs[0]
	for _, n := range navs {
		for _, a := range n.Attr {
			if strings.HasSuffix(a.Key, "type") && strings.Contains(" "+a.Val+" ", " toc ") {
				nav = n
			}
		}
	}
	var out []navLink
	var walk func(n *html.Node, depth int)
	walk = func(n *html.Node, depth int) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "ol", "ul":
				depth++
			case "a":
				for _, a := range n.Attr {
					if a.Key == "href" {
						out = append(out, navLink{level: depth, title: nodeText(n), href: a.Val})
					}
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, depth)
		}
	}
	walk(nav, 0)
	return out
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// resolve turns a link in a file under dir into a path in the archive,
// without its fragment.
func resolve(dir, href string) string {
	href = strings.SplitN(href, "#", 2)[0]
	if u, err := url.PathUnescape(href); err == nil {
		href = u
	}
	return path.Clean(path.Join(dir, href))
}

var (
	mdHeading = regexp.MustCompile(`^ {0,3}(#{1,3})[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$`)
	mdFence   = regexp.MustCompile("^ {0,3}(```|~~~)")
)

// markdownHeadings lists the ATX headings of levels 1–3 on each page,
// skipping fenced code.
func markdownHeadings(pages []knowledgedomain.Page) []knowledgedomain.Heading {
	var (
		out   []knowledgedomain.Heading
		fence string
	)
	for _, p := range pages {
		for line := range strings.SplitSeq(p.Text, "\n") {
			if m := mdFence.FindStringSubmatch(line); m != nil {
				switch fence {
				case "":
					fence = m[1]
				case m[1]:
					fence = ""
				}
				continue
			}
			if fence != "" {
				continue
			}
			if m := mdHeading.FindStringSubmatch(line); m != nil {
				out = append(out, knowledgedomain.Heading{Level: len(m[1]), Title: m[2], Page: p.Number})
			}
		}
	}
	return out
}
