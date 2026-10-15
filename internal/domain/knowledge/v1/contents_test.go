package knowledge_test

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	knowledge "kurgu/internal/domain/knowledge/v1"
)

func TestNormalizeHeadings(t *testing.T) {
	got := knowledge.NormalizeHeadings([]knowledge.Heading{
		{Level: 2, Title: "  1   Introduction ", Page: 3},
		{Level: 3, Title: "Binary search", Page: 5},
		{Level: 2, Title: "", Page: 9},            // no title
		{Level: 2, Title: "Index", Page: 400},     // off the document
		{Level: 2, Title: "Cover", Page: 0},       // no page
		{Level: 9, Title: "Deep", Page: 6},        // capped
		{Level: 2, Title: "2 Sorting", Page: 12},
	}, 100)
	want := []knowledge.Heading{
		{Level: 1, Title: "1 Introduction", Page: 3},
		{Level: 2, Title: "Binary search", Page: 5},
		{Level: 6, Title: "Deep", Page: 6},
		{Level: 1, Title: "2 Sorting", Page: 12},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}

	long := knowledge.NormalizeHeadings([]knowledge.Heading{{Level: 1, Title: strings.Repeat("ş", 400), Page: 1}}, 1)
	if n := utf8.RuneCountInString(long[0].Title); n != 300 || !strings.HasSuffix(long[0].Title, "…") {
		t.Fatalf("long title has %d runes: %q", n, long[0].Title[:20])
	}

	many := make([]knowledge.Heading, knowledge.MaxHeadings+5)
	for i := range many {
		many[i] = knowledge.Heading{Level: 1, Title: "x", Page: 1}
	}
	if n := len(knowledge.NormalizeHeadings(many, 1)); n != knowledge.MaxHeadings {
		t.Fatalf("kept %d headings", n)
	}
}

func TestContents(t *testing.T) {
	got := knowledge.Contents([]knowledge.Heading{
		{Level: 1, Title: "Front matter", Page: 1},
		{Level: 1, Title: "1 Introduction", Page: 27},
		{Level: 2, Title: "Binary search", Page: 29},
		{Level: 2, Title: "Big O", Page: 36},
		{Level: 3, Title: "Same page", Page: 36},
		{Level: 2, Title: "Also on 36", Page: 36},
		{Level: 1, Title: "2 Selection sort", Page: 47},
	}, 60)
	want := []knowledge.Section{
		{Level: 1, Title: "Front matter", From: 1, To: 26},
		{Level: 1, Title: "1 Introduction", From: 27, To: 46},
		{Level: 2, Title: "Binary search", From: 29, To: 35},
		{Level: 2, Title: "Big O", From: 36, To: 36},
		{Level: 3, Title: "Same page", From: 36, To: 36},
		{Level: 2, Title: "Also on 36", From: 36, To: 46},
		{Level: 1, Title: "2 Selection sort", From: 47, To: 60},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}
