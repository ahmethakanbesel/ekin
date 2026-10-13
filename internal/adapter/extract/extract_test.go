package extract_test

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kurgu/internal/adapter/extract"
	knowledgedomain "kurgu/internal/domain/knowledge/v1"
)

func TestPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	pages, err := extract.New().Extract(context.Background(), knowledgedomain.FormatPDF, "testdata/three-pages.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 || !strings.Contains(pages[1].Text, "bağlanma stilleri") || pages[2].Number != 3 {
		t.Fatalf("pages = %+v", pages)
	}
	if strings.Contains(pages[1].Text, "\n") && !strings.Contains(pages[1].Text, "\n\n") {
		t.Fatalf("soft line breaks kept: %q", pages[1].Text)
	}
}

func TestMissingPDFTool(t *testing.T) {
	e := &extract.Extractor{PDFToText: "kurgu-no-such-binary"}
	if _, err := e.Extract(context.Background(), knowledgedomain.FormatPDF, "testdata/three-pages.pdf"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err = %v", err)
	}
}

func TestEPUB(t *testing.T) {
	p := filepath.Join(t.TempDir(), "book.epub")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	add := func(name, body string) {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(body))
	}
	add("mimetype", "application/epub+zip")
	add("META-INF/container.xml", `<?xml version="1.0"?><container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`)
	add("OEBPS/content.opf", `<?xml version="1.0"?><package><manifest>
		<item id="c2" href="text/ch2.xhtml"/><item id="c1" href="text/ch1.xhtml"/><item id="css" href="s.css"/>
	</manifest><spine><itemref idref="c1"/><itemref idref="c2"/></spine></package>`)
	add("OEBPS/text/ch1.xhtml", `<html><head><style>p{}</style></head><body><h1>Bölüm 1</h1><p>Kaygı   nedir?</p><p>İkinci <b>paragraf</b>.</p></body></html>`)
	add("OEBPS/text/ch2.xhtml", `<html><body><h1>Bölüm 2</h1><p>`+strings.Repeat("Uzun bir cümle. ", 300)+`</p></body></html>`)
	_ = z.Close()
	_ = f.Close()

	pages, err := extract.New().Extract(context.Background(), knowledgedomain.FormatEPUB, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 3 {
		t.Fatalf("pages = %d", len(pages))
	}
	if pages[0].Text != "Bölüm 1\n\nKaygı nedir?\n\nİkinci paragraf ." && pages[0].Text != "Bölüm 1\n\nKaygı nedir?\n\nİkinci paragraf." {
		t.Fatalf("chapter 1 = %q", pages[0].Text)
	}
	if !strings.HasPrefix(pages[1].Text, "Bölüm 2") {
		t.Fatalf("chapter 2 does not start a page: %q", pages[1].Text[:20])
	}
}

func TestText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.md")
	_ = os.WriteFile(p, []byte("\xef\xbb\xbf# Başlık\n\nMetin."), 0o600)
	pages, err := extract.New().Extract(context.Background(), knowledgedomain.FormatMarkdown, p)
	if err != nil || len(pages) != 1 || pages[0].Text != "# Başlık\n\nMetin." {
		t.Fatalf("pages = %+v, %v", pages, err)
	}
	_ = os.WriteFile(p, []byte{0xff, 0xfe, 0x00}, 0o600)
	if _, err := extract.New().Extract(context.Background(), knowledgedomain.FormatText, p); err == nil {
		t.Fatal("binary text accepted")
	}
}
