package extract_test

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ekin/internal/adapter/extract"
	knowledgedomain "ekin/internal/domain/knowledge/v1"
)

func TestPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	got, err := extract.New().Extract(context.Background(), knowledgedomain.FormatPDF, "testdata/three-pages.pdf")
	if err != nil {
		t.Fatal(err)
	}
	pages := got.Pages
	if len(got.Headings) != 0 {
		t.Fatalf("a PDF without bookmarks has headings %+v", got.Headings)
	}
	if len(pages) != 3 || !strings.Contains(pages[1].Text, "bağlanma stilleri") || pages[2].Number != 3 {
		t.Fatalf("pages = %+v", pages)
	}
	if strings.Contains(pages[1].Text, "\n") && !strings.Contains(pages[1].Text, "\n\n") {
		t.Fatalf("soft line breaks kept: %q", pages[1].Text)
	}
}

func TestMissingPDFTool(t *testing.T) {
	e := &extract.Extractor{PDFToText: "ekin-no-such-binary"}
	if _, err := e.Extract(context.Background(), knowledgedomain.FormatPDF, "testdata/three-pages.pdf"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err = %v", err)
	}
}

// writeEPUB zips files (path → body) into an EPUB and returns its path.
func writeEPUB(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "book.epub")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	w, _ := z.Create("mimetype")
	_, _ = w.Write([]byte("application/epub+zip"))
	for name, body := range files {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = z.Close()
	_ = f.Close()
	return p
}

const epubContainer = `<?xml version="1.0"?><container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`

func TestEPUB(t *testing.T) {
	p := writeEPUB(t, map[string]string{
		"META-INF/container.xml": epubContainer,
		"OEBPS/content.opf": `<?xml version="1.0"?><package><manifest>
		<item id="c2" href="text/ch2.xhtml"/><item id="c1" href="text/ch1.xhtml"/><item id="css" href="s.css"/>
		<item id="nav" href="nav.xhtml" properties="nav"/>
	</manifest><spine><itemref idref="c1"/><itemref idref="c2"/></spine></package>`,
		"OEBPS/nav.xhtml": `<html xmlns:epub="http://www.idpf.org/2007/ops"><body>
		<nav epub:type="landmarks"><ol><li><a href="text/ch2.xhtml">Wrong list</a></li></ol></nav>
		<nav epub:type="toc"><ol>
			<li><a href="text/ch1.xhtml">Bölüm <em>1</em></a></li>
			<li><a href="text/ch2.xhtml#top">Bölüm 2</a><ol><li><a href="text/ch2.xhtml#s1">Alt başlık</a></li></ol></li>
			<li><a href="text/missing.xhtml">Kayıp</a></li>
		</ol></nav></body></html>`,
		"OEBPS/text/ch1.xhtml": `<html><head><style>p{}</style></head><body><h1>Bölüm 1</h1><p>Kaygı   nedir?</p><p>İkinci <b>paragraf</b>.</p></body></html>`,
		"OEBPS/text/ch2.xhtml": `<html><body><h1>Bölüm 2</h1><p>` + strings.Repeat("Uzun bir cümle. ", 300) + `</p></body></html>`,
	})

	got, err := extract.New().Extract(context.Background(), knowledgedomain.FormatEPUB, p)
	if err != nil {
		t.Fatal(err)
	}
	pages := got.Pages
	if len(pages) < 3 {
		t.Fatalf("pages = %d", len(pages))
	}
	if pages[0].Text != "Bölüm 1\n\nKaygı nedir?\n\nİkinci paragraf ." && pages[0].Text != "Bölüm 1\n\nKaygı nedir?\n\nİkinci paragraf." {
		t.Fatalf("chapter 1 = %q", pages[0].Text)
	}
	if !strings.HasPrefix(pages[1].Text, "Bölüm 2") {
		t.Fatalf("chapter 2 does not start a page: %q", pages[1].Text[:20])
	}
	want := []knowledgedomain.Heading{
		{Level: 1, Title: "Bölüm 1", Page: 1},
		{Level: 1, Title: "Bölüm 2", Page: 2},
		{Level: 2, Title: "Alt başlık", Page: 2},
	}
	if !reflect.DeepEqual(got.Headings, want) {
		t.Fatalf("headings = %+v", got.Headings)
	}
}

func TestEPUB2NCX(t *testing.T) {
	p := writeEPUB(t, map[string]string{
		"META-INF/container.xml": epubContainer,
		"OEBPS/content.opf": `<?xml version="1.0"?><package><manifest>
		<item id="c1" href="ch%201.html"/><item id="c2" href="ch2.html"/><item id="ncx" href="toc.ncx"/>
	</manifest><spine toc="ncx"><itemref idref="c1"/><itemref idref="c2"/></spine></package>`,
		"OEBPS/toc.ncx": `<?xml version="1.0"?><ncx><navMap>
		<navPoint><navLabel><text>Part One</text></navLabel><content src="ch%201.html"/>
			<navPoint><navLabel><text>Two</text></navLabel><content src="ch2.html#x"/></navPoint>
		</navPoint></navMap></ncx>`,
		"OEBPS/ch 1.html": `<html><body><p>One.</p></body></html>`,
		"OEBPS/ch2.html":  `<html><body><p>Two.</p></body></html>`,
	})
	got, err := extract.New().Extract(context.Background(), knowledgedomain.FormatEPUB, p)
	if err != nil {
		t.Fatal(err)
	}
	want := []knowledgedomain.Heading{{Level: 1, Title: "Part One", Page: 1}, {Level: 2, Title: "Two", Page: 2}}
	if !reflect.DeepEqual(got.Headings, want) {
		t.Fatalf("headings = %+v", got.Headings)
	}
}

func TestText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.md")
	_ = os.WriteFile(p, []byte("\xef\xbb\xbf# Başlık\n\nMetin."), 0o600)
	got, err := extract.New().Extract(context.Background(), knowledgedomain.FormatMarkdown, p)
	if err != nil || len(got.Pages) != 1 || got.Pages[0].Text != "# Başlık\n\nMetin." {
		t.Fatalf("pages = %+v, %v", got.Pages, err)
	}

	md := "# Kaygı\n\nGiriş.\n\n```sh\n# not a heading\n```\n\n## Belirtiler ##\n\n" + strings.Repeat("Uzun bir paragraf. ", 200) +
		"\n\n#### Too deep\n\n#No space\n\n   ### Tedavi\n"
	_ = os.WriteFile(p, []byte(md), 0o600)
	got, err = extract.New().Extract(context.Background(), knowledgedomain.FormatMarkdown, p)
	if err != nil {
		t.Fatal(err)
	}
	want := []knowledgedomain.Heading{
		{Level: 1, Title: "Kaygı", Page: 1},
		{Level: 2, Title: "Belirtiler", Page: 1},
		{Level: 3, Title: "Tedavi", Page: got.Pages[len(got.Pages)-1].Number},
	}
	if len(got.Pages) < 2 || !reflect.DeepEqual(got.Headings, want) {
		t.Fatalf("%d pages, headings = %+v", len(got.Pages), got.Headings)
	}
	_ = os.WriteFile(p, []byte(md), 0o600)
	if got, _ := extract.New().Extract(context.Background(), knowledgedomain.FormatText, p); len(got.Headings) != 0 {
		t.Fatalf("plain text has headings %+v", got.Headings)
	}
	_ = os.WriteFile(p, []byte{0xff, 0xfe, 0x00}, 0o600)
	if _, err := extract.New().Extract(context.Background(), knowledgedomain.FormatText, p); err == nil {
		t.Fatal("binary text accepted")
	}
}
