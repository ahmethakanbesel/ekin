package extract

import (
	"reflect"
	"strings"
	"testing"

	knowledgedomain "kurgu/internal/domain/knowledge/v1"
)

// The tail of `pdftohtml -xml -i -stdout -f 1 -l 1` on a book with
// bookmarks.
const pdfXML = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pdf2xml SYSTEM "pdf2xml.dtd">
<pdf2xml producer="poppler" version="24.04.0">
<page number="1" position="absolute" top="0" left="0" height="1188" width="918">
<text top="10" left="10" width="100" height="20" font="0"><b>grokking</b> algorithms &amp; more</text>
</page>
<outline>
<item page="1">grokking algorithms</item>
<item page="27">1 introduction to algorithms</item>
<outline>
<item page="29">Binary search </item>
<item page="36">Big O &amp; you</item>
</outline>
<item>An external link</item>
<item page="47">2 selection sort</item>
</outline>
</pdf2xml>`

func TestParsePDFOutline(t *testing.T) {
	want := []knowledgedomain.Heading{
		{Level: 1, Title: "grokking algorithms", Page: 1},
		{Level: 1, Title: "1 introduction to algorithms", Page: 27},
		{Level: 2, Title: "Binary search ", Page: 29},
		{Level: 2, Title: "Big O & you", Page: 36},
		{Level: 1, Title: "2 selection sort", Page: 47},
	}
	if got := outlineOf(pdfXML); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := outlineOf(`<pdf2xml><page number="1"></page></pdf2xml>`); len(got) != 0 {
		t.Fatalf("no outline gave %+v", got)
	}
}

func outlineOf(doc string) []knowledgedomain.Heading {
	outline, _ := parsePDFXML(strings.NewReader(doc))
	return outline
}
