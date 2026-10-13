package knowledge_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"kurgu/internal/apperror"
	knowledge "kurgu/internal/domain/knowledge/v1"
	"kurgu/pkg/id"
)

var now = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

func TestFormatFromFilename(t *testing.T) {
	tests := map[string]knowledge.Format{
		"a.PDF": knowledge.FormatPDF, "b.txt": knowledge.FormatText, "c.markdown": knowledge.FormatMarkdown,
		"d.md": knowledge.FormatMarkdown, "e.epub": knowledge.FormatEPUB,
	}
	for name, want := range tests {
		if got, err := knowledge.FormatFromFilename(name); err != nil || got != want {
			t.Errorf("%s = %s, %v", name, got, err)
		}
	}
	if _, err := knowledge.FormatFromFilename("x.docx"); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("docx accepted: %v", err)
	}
}

func TestDocumentLifecycle(t *testing.T) {
	d, err := knowledge.New(id.New(), id.New(), id.New(), "", knowledge.Upload{Filename: "dir/Kaygı Kitabı.pdf", Size: 10}, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title() != "Kaygı Kitabı" || d.Filename() != "Kaygı Kitabı.pdf" || d.Status() != knowledge.StatusPending {
		t.Fatalf("new = %+v", d.Snapshot())
	}
	if err := d.CheckRange(0, 0); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("pending doc referenced: %v", err)
	}
	pages := []knowledge.Page{knowledge.NewPage(1, "bir iki"), knowledge.NewPage(2, "üç dört beş")}
	if err := d.Extracted(pages, now); err != nil {
		t.Fatal(err)
	}
	if d.Status() != knowledge.StatusReady || d.PageCount() != 2 || d.Tokens() != pages[0].Tokens+pages[1].Tokens {
		t.Fatalf("extracted = %+v", d.Snapshot())
	}
	tests := []struct {
		from, to int
		ok       bool
	}{{0, 0, true}, {1, 2, true}, {2, 2, true}, {1, 3, false}, {2, 1, false}, {0, 1, false}}
	for _, tt := range tests {
		if err := d.CheckRange(tt.from, tt.to); (err == nil) != tt.ok {
			t.Errorf("range %d-%d err = %v", tt.from, tt.to, err)
		}
	}
	if err := d.Retry(now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("retry ready = %v", err)
	}
	_ = d.Extracted(nil, now)
	if d.Status() != knowledge.StatusFailed || d.Failure() == "" {
		t.Fatalf("empty extraction = %+v", d.Snapshot())
	}
	if err := d.Retry(now); err != nil || d.Status() != knowledge.StatusPending {
		t.Fatalf("retry = %v", err)
	}
	got, err := knowledge.Rehydrate(d.Snapshot())
	if err != nil || got.Snapshot() != d.Snapshot() {
		t.Fatalf("rehydrate = %+v, %v", got, err)
	}
	if _, err := knowledge.New(id.New(), id.New(), id.New(), "", knowledge.Upload{Filename: "big.pdf", Size: knowledge.MaxFileBytes + 1}, now); err == nil {
		t.Fatal("oversized upload accepted")
	}
}

func TestEstimateTokens(t *testing.T) {
	en := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 20) // 180 words, ~900 chars
	tr := strings.Repeat("Kaygı bozukluğu yaşayan bireylerde düşünceler hızlanır. ", 20)
	if n := knowledge.EstimateTokens(en); n < 200 || n > 320 {
		t.Errorf("english estimate %d out of the expected band", n)
	}
	if knowledge.EstimateTokens(tr) <= knowledge.EstimateTokens(strings.Repeat("Anxiety disorders make thoughts race faster. ", 20)) {
		t.Error("turkish should cost more tokens than comparable english")
	}
	if knowledge.EstimateTokens("") != 0 {
		t.Error("empty text has tokens")
	}
}

func TestPaginate(t *testing.T) {
	para := strings.Repeat("Bir cümle burada. ", 40) // ~720 runes
	text := strings.Repeat(para+"\n\n", 10)          // ~7200 runes
	text += strings.Repeat("uzun", 2000)             // one 8000-rune word: hard cut
	pages := knowledge.Paginate(text)
	if len(pages) < 5 {
		t.Fatalf("pages = %d", len(pages))
	}
	for i, p := range pages {
		if p.Number != i+1 || utf8.RuneCountInString(p.Text) > knowledge.PageTargetRunes || p.Text == "" {
			t.Fatalf("page %d: number %d, %d runes", i, p.Number, utf8.RuneCountInString(p.Text))
		}
	}
	if knowledge.Paginate("  \n\n ") != nil {
		t.Fatal("blank text produced pages")
	}
}
