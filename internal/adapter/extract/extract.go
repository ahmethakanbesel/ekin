// Package extract pulls text out of uploaded documents: PDF through
// poppler's pdftotext (page by page), EPUB by walking the spine, and
// plain text / Markdown as they are.
package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	knowledgedomain "kurgu/internal/domain/knowledge/v1"
)

// Extractor runs the tools. PDFToText is the pdftotext binary.
type Extractor struct {
	PDFToText string
}

// New returns an Extractor using pdftotext from PATH.
func New() *Extractor { return &Extractor{PDFToText: "pdftotext"} }

// Extract returns the pages of the file at p.
func (e *Extractor) Extract(ctx context.Context, format knowledgedomain.Format, p string) ([]knowledgedomain.Page, error) {
	switch format {
	case knowledgedomain.FormatPDF:
		texts, err := e.pdf(ctx, p)
		if err != nil {
			return nil, err
		}
		return knowledgedomain.FromPrinted(texts), nil
	case knowledgedomain.FormatEPUB:
		sections, err := epub(p)
		if err != nil {
			return nil, err
		}
		return knowledgedomain.FromSections(sections), nil
	case knowledgedomain.FormatText, knowledgedomain.FormatMarkdown:
		b, err := os.ReadFile(p) //nolint:gosec // a temp file the worker wrote
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(b) {
			return nil, errors.New("the file is not UTF-8 text")
		}
		return knowledgedomain.FromSections([]string{string(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")))}), nil
	}
	return nil, fmt.Errorf("unsupported format %q", format)
}

// pdf runs pdftotext once; pages come back separated by form feeds.
func (e *Extractor) pdf(ctx context.Context, p string) ([]string, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, e.PDFToText, "-enc", "UTF-8", "-q", p, "-") //nolint:gosec // fixed binary, temp path
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("pdftotext is not installed (poppler-utils)")
		}
		return nil, fmt.Errorf("pdftotext: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	pages := strings.Split(string(out), "\f")
	if len(pages) > 0 && strings.TrimSpace(pages[len(pages)-1]) == "" {
		pages = pages[:len(pages)-1] // pdftotext ends every page, the last included, with \f
	}
	for i, pg := range pages {
		pages[i] = tidyPDF(pg)
	}
	return pages, nil
}

// tidyPDF rejoins hyphenated line breaks and single newlines inside
// paragraphs, keeping blank-line paragraph breaks.
func tidyPDF(s string) string {
	s = strings.ReplaceAll(s, "-\n", "")
	var b strings.Builder
	for i, para := range strings.Split(s, "\n\n") {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.Join(strings.Fields(para), " "))
	}
	return strings.TrimSpace(b.String())
}

// --- EPUB

type container struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opf struct {
	Manifest []struct {
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

const maxEPUBEntry = 20 << 20

func epub(p string) ([]string, error) {
	z, err := zip.OpenReader(p)
	if err != nil {
		return nil, fmt.Errorf("not a valid EPUB: %w", err)
	}
	defer func() { _ = z.Close() }()
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("EPUB is missing %s", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(io.LimitReader(rc, maxEPUBEntry))
	}
	raw, err := read("META-INF/container.xml")
	if err != nil {
		return nil, err
	}
	var c container
	if err := xml.Unmarshal(raw, &c); err != nil || len(c.Rootfiles) == 0 {
		return nil, errors.New("EPUB container.xml has no rootfile")
	}
	opfPath := c.Rootfiles[0].FullPath
	raw, err = read(opfPath)
	if err != nil {
		return nil, err
	}
	var pkg opf
	if err := xml.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("EPUB package: %w", err)
	}
	hrefs := map[string]string{}
	for _, it := range pkg.Manifest {
		hrefs[it.ID] = it.Href
	}
	base := path.Dir(opfPath)
	var sections []string
	for _, ref := range pkg.Spine {
		href, ok := hrefs[ref.IDRef]
		if !ok {
			continue
		}
		name := path.Clean(path.Join(base, strings.SplitN(href, "#", 2)[0]))
		doc, err := read(name)
		if err != nil {
			return nil, err
		}
		if text := htmlText(doc); text != "" {
			sections = append(sections, text)
		}
	}
	return sections, nil
}

// htmlText flattens XHTML to paragraphs separated by blank lines.
func htmlText(doc []byte) string {
	root, err := html.Parse(bytes.NewReader(doc))
	if err != nil {
		return ""
	}
	var (
		b    strings.Builder
		line strings.Builder
	)
	flush := func() {
		if t := strings.Join(strings.Fields(line.String()), " "); t != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(t)
		}
		line.Reset()
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "head":
				return
			case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "blockquote", "br", "tr", "section":
				flush()
				defer flush()
			}
		}
		if n.Type == html.TextNode {
			line.WriteString(n.Data)
			line.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	flush()
	return b.String()
}
