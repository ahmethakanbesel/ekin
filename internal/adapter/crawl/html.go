package crawl

import (
	"bytes"
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// facts is what a page's HTML says about it.
type facts struct {
	title, description, canonical, lang string
	h1                                  []string
	noindex                             bool
	words                               int
	types                               []string // JSON-LD @type values
	links                               []string // absolute hrefs as written, resolved
}

// extract reads an HTML page fetched from base.
func extract(body []byte, base *url.URL) facts {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return facts{}
	}
	var f facts
	var text strings.Builder
	var walk func(n *html.Node, inHead bool)
	walk = func(n *html.Node, inHead bool) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Html:
				f.lang = strings.TrimSpace(attr(n, "lang"))
			case atom.Head:
				inHead = true
			case atom.Base:
				if b, err := base.Parse(attr(n, "href")); err == nil && attr(n, "href") != "" {
					base = b
				}
			case atom.Title:
				if f.title == "" && !insideSVG(n) {
					f.title = collapse(textOf(n))
				}
			case atom.Meta:
				switch strings.ToLower(attr(n, "name")) {
				case "description":
					if f.description == "" {
						f.description = collapse(attr(n, "content"))
					}
				case "robots":
					if hasNoindex(attr(n, "content")) {
						f.noindex = true
					}
				}
			case atom.Link:
				if f.canonical == "" && relHas(attr(n, "rel"), "canonical") {
					if u, err := base.Parse(strings.TrimSpace(attr(n, "href"))); err == nil && attr(n, "href") != "" {
						f.canonical = u.String()
					}
				}
			case atom.H1:
				// An empty heading (a logo image) still counts as one.
				f.h1 = append(f.h1, collapse(textOf(n)))
			case atom.A:
				if href := strings.TrimSpace(attr(n, "href")); href != "" && !strings.HasPrefix(href, "#") {
					if u, err := base.Parse(strings.TrimSpace(href)); err == nil {
						f.links = append(f.links, u.String())
					}
				}
			case atom.Script:
				if strings.EqualFold(strings.TrimSpace(attr(n, "type")), "application/ld+json") {
					f.types = append(f.types, ldTypes(textOf(n))...)
				}
				return
			case atom.Style, atom.Noscript, atom.Template, atom.Svg, atom.Iframe:
				return
			}
		}
		if n.Type == html.TextNode && !inHead {
			text.WriteString(n.Data)
			text.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inHead)
		}
	}
	walk(doc, false)
	f.words = len(strings.Fields(text.String()))
	slices.Sort(f.types)
	f.types = slices.Compact(f.types)
	return f
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func insideSVG(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.DataAtom == atom.Svg {
			return true
		}
	}
	return false
}

// collapse trims s and folds runs of whitespace into one space.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func relHas(rel, want string) bool {
	return slices.ContainsFunc(strings.Fields(rel), func(r string) bool { return strings.EqualFold(r, want) })
}

// hasNoindex reads a robots meta tag or X-Robots-Tag value.
func hasNoindex(v string) bool {
	for _, d := range strings.FieldsFunc(strings.ToLower(v), func(r rune) bool { return r == ',' || r == ' ' }) {
		if d == "noindex" || d == "none" {
			return true
		}
	}
	return false
}

// ldTypes collects the @type values of a JSON-LD block, including those
// in an @graph and nested objects. A block that does not parse adds
// nothing.
func ldTypes(raw string) []string {
	var v any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &v) != nil {
		return nil
	}
	var out []string
	var walk func(any, int)
	walk = func(v any, depth int) {
		if depth > 8 {
			return
		}
		switch t := v.(type) {
		case []any:
			for _, e := range t {
				walk(e, depth+1)
			}
		case map[string]any:
			switch ty := t["@type"].(type) {
			case string:
				out = append(out, ty)
			case []any:
				for _, e := range ty {
					if s, ok := e.(string); ok {
						out = append(out, s)
					}
				}
			}
			for k, e := range t {
				if k != "@type" && k != "@context" {
					walk(e, depth+1)
				}
			}
		}
	}
	walk(v, 0)
	return out
}
