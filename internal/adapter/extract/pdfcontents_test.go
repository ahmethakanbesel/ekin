package extract

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	knowledgedomain "kurgu/internal/domain/knowledge/v1"
)

// book is pdftohtml XML for a 4-page book: body text in 18 pt, a 25 pt
// chapter title, 21 pt section headings (one over two lines), a 14 pt
// running header on every page and a large-font pull quote too long to
// be a heading. Its bookmarks name only the chapters.
func book(outline string) string {
	line := func(top int) string {
		return fmt.Sprintf(`<text top="%d" left="0" width="500" height="20" font="1">Body text that runs on and on, as body text does.</text>`, top)
	}
	header := `<text top="5" left="0" width="100" height="14" font="3">Design Consistent Hashing</text>`
	return `<pdf2xml>
<page number="1"><fontspec id="0" size="25"/><fontspec id="1" size="18"/>` + header + `
<text top="40" left="0" width="400" height="30" font="0"><b>CHAPTER 1: HASHING</b></text>` + line(100) + line(120) + `</page>
<page number="2"><fontspec id="2" size="21"/><fontspec id="3" size="14"/>` + header + `
<text top="40" left="0" width="400" height="24" font="2"><b>The rehashing</b></text>
<text top="64" left="0" width="400" height="24" font="2"><b>problem</b></text>` + line(100) + line(120) + `
<text top="200" left="0" width="400" height="24" font="2"><b>Virtual </b></text><text top="200" left="80" width="40" height="24" font="2"><i>nodes</i></text>` + line(240) + `</page>
<page number="3">` + header + `
<text top="40" left="0" width="400" height="24" font="2">A pull quote set large</text>
<text top="64" left="0" width="400" height="24" font="2">that runs over</text>
<text top="88" left="0" width="400" height="24" font="2">four lines, so it is</text>
<text top="112" left="0" width="400" height="24" font="2">not a heading</text>` + line(140) + `
<text top="200" left="0" width="400" height="24" font="2">42</text>
<text top="240" left="0" width="400" height="30" font="0"><b>CHAPTER 2: TREES</b></text>` + line(280) + `</page>
<page number="4">` + header + `<text top="40" left="0" width="400" height="24" font="2">Fanout</text>` + line(100) + line(120) + `</page>
` + outline + `</pdf2xml>`
}

func TestPDFContents(t *testing.T) {
	tests := []struct {
		name    string
		outline string
		want    []knowledgedomain.Heading
	}{
		{
			name:    "chapter bookmarks get their sections",
			outline: `<outline><item page="1">CHAPTER 1: HASHING</item><item page="3">CHAPTER 2: TREES</item></outline>`,
			want: []knowledgedomain.Heading{
				{Level: 1, Title: "CHAPTER 1: HASHING", Page: 1},
				{Level: 2, Title: "The rehashing problem", Page: 2},
				{Level: 2, Title: "Virtual nodes", Page: 2},
				{Level: 1, Title: "CHAPTER 2: TREES", Page: 3},
				{Level: 2, Title: "Fanout", Page: 4},
			},
		},
		{
			name: "bookmarks with children keep them",
			outline: `<outline><item page="1">CHAPTER 1: HASHING</item><outline><item page="2">Rehashing</item></outline>
				<item page="3">CHAPTER 2: TREES</item></outline>`,
			want: []knowledgedomain.Heading{
				{Level: 1, Title: "CHAPTER 1: HASHING", Page: 1},
				{Level: 2, Title: "Rehashing", Page: 2},
				{Level: 3, Title: "The rehashing problem", Page: 2},
				{Level: 3, Title: "Virtual nodes", Page: 2},
				{Level: 1, Title: "CHAPTER 2: TREES", Page: 3},
				{Level: 2, Title: "Fanout", Page: 4},
			},
		},
		{
			name: "no bookmarks: headings by size",
			want: []knowledgedomain.Heading{
				{Level: 1, Title: "CHAPTER 1: HASHING", Page: 1},
				{Level: 2, Title: "The rehashing problem", Page: 2},
				{Level: 2, Title: "Virtual nodes", Page: 2},
				{Level: 1, Title: "CHAPTER 2: TREES", Page: 3},
				{Level: 2, Title: "Fanout", Page: 4},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outline, lines := parsePDFXML(strings.NewReader(book(tt.outline)))
			got := mergeContents(knowledgedomain.NormalizeHeadings(outline, 4), detectHeadings(lines), 4)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}
