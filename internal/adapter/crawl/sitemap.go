package crawl

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"io"
	"strings"
)

// Sitemap limits: the protocol caps a file at 50,000 URLs; Ekin reads at
// most this many files per site.
const (
	maxSitemapFiles = 50
	maxSitemapURLs  = 50_000
	maxSitemapBytes = 50 << 20
)

type sitemapDoc struct {
	XMLName  xml.Name
	URLs     []struct{ Loc string `xml:"loc"` } `xml:"url"`
	Sitemaps []struct{ Loc string `xml:"loc"` } `xml:"sitemap"`
}

// parseSitemap reads a urlset or a sitemapindex, gzipped or not, and
// returns its page URLs and child sitemap URLs. A file that is neither
// returns nothing.
func parseSitemap(body []byte) (pages, children []string) {
	if len(body) > 2 && body[0] == 0x1f && body[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, nil
		}
		body, err = io.ReadAll(io.LimitReader(zr, maxSitemapBytes))
		if err != nil {
			return nil, nil
		}
	}
	var doc sitemapDoc
	if xml.Unmarshal(body, &doc) != nil {
		return nil, nil
	}
	switch doc.XMLName.Local {
	case "urlset":
		for _, u := range doc.URLs {
			if loc := strings.TrimSpace(u.Loc); loc != "" {
				pages = append(pages, loc)
			}
		}
	case "sitemapindex":
		for _, s := range doc.Sitemaps {
			if loc := strings.TrimSpace(s.Loc); loc != "" {
				children = append(children, loc)
			}
		}
	}
	return pages, children
}
