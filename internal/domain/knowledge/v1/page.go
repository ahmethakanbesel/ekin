package knowledge

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Page is one page of extracted text. For formats without pages (TXT,
// MD, EPUB) a page is a chunk of about PageTargetRunes characters cut at
// paragraph boundaries.
type Page struct {
	Number int
	Text   string
	Words  int
	Tokens int
}

// PageTargetRunes is the size of a synthetic page: about one printed
// book page.
const PageTargetRunes = 2500

// NewPage measures text and returns page number n.
func NewPage(n int, text string) Page {
	text = strings.TrimSpace(text)
	words := len(strings.Fields(text))
	return Page{Number: n, Text: text, Words: words, Tokens: EstimateTokens(text)}
}

// EstimateTokens approximates the token count of text for Claude-class
// tokenizers, which publish no offline tokenizer. Latin-script prose
// averages about 4 characters per token in English and closer to 3 in
// Turkish (agglutinative words split into more pieces); digits and
// punctuation cost more. The estimate is within about ±20% on mixed
// Turkish/English prose, which is enough to budget an agent's context.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var letters, other, nonASCII int
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
		case unicode.IsLetter(r):
			letters++
			if r >= utf8.RuneSelf {
				nonASCII++
			}
		default:
			other++
		}
	}
	words := len(strings.Fields(text))
	// Letters at ~3.6 per token, a token per word boundary's worth of
	// splitting, punctuation and digits close to one each.
	est := float64(letters)/3.6 + float64(other)*0.7 + float64(nonASCII)*0.15 + float64(words)*0.1
	return int(math.Ceil(est))
}

// FromPrinted numbers printed pages (PDF) as they are, keeping empty
// pages so numbers match the original. Returns nil if every page is
// blank.
func FromPrinted(texts []string) []Page {
	pages := make([]Page, 0, len(texts))
	blank := true
	for i, t := range texts {
		p := NewPage(i+1, t)
		if p.Text != "" {
			blank = false
		}
		pages = append(pages, p)
	}
	if blank {
		return nil
	}
	return pages
}

// Extraction is what a file yields: its pages and, when the file
// declares one, its table of contents.
type Extraction struct {
	Pages    []Page
	Headings []Heading
}

// FromSections paginates each section (an EPUB chapter, a whole text
// file) and numbers the pages continuously, so pages never straddle a
// chapter.
func FromSections(sections []string) []Page {
	pages, _ := PaginateSections(sections)
	return pages
}

// PaginateSections is FromSections that also returns the page each
// section starts on (for an empty section, the page after it).
func PaginateSections(sections []string) (pages []Page, starts []int) {
	starts = make([]int, len(sections))
	for i, sec := range sections {
		starts[i] = len(pages) + 1
		for _, p := range Paginate(sec) {
			p.Number = len(pages) + 1
			pages = append(pages, p)
		}
	}
	return pages, starts
}

// Paginate splits continuous text into synthetic pages at paragraph
// boundaries (blank lines), keeping each near PageTargetRunes. Overlong
// paragraphs are split at sentence ends, then at spaces.
func Paginate(text string) []Page {
	var (
		pages []Page
		cur   strings.Builder
	)
	flush := func() {
		if strings.TrimSpace(cur.String()) != "" {
			pages = append(pages, NewPage(len(pages)+1, cur.String()))
		}
		cur.Reset()
	}
	for _, para := range splitParagraphs(text) {
		for _, piece := range splitLong(para, PageTargetRunes) {
			n := utf8.RuneCountInString(piece)
			if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+n > PageTargetRunes {
				flush()
			}
			if cur.Len() > 0 {
				cur.WriteString("\n\n")
			}
			cur.WriteString(piece)
		}
	}
	flush()
	return pages
}

func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	for p := range strings.SplitSeq(text, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitLong cuts s into pieces of at most limit runes, preferring
// sentence ends, then spaces.
func splitLong(s string, limit int) []string {
	var out []string
	for utf8.RuneCountInString(s) > limit {
		runes := []rune(s)
		cut := -1
		for i := limit; i > limit/2; i-- {
			if (runes[i-1] == '.' || runes[i-1] == '!' || runes[i-1] == '?') && unicode.IsSpace(runes[i]) {
				cut = i
				break
			}
		}
		if cut < 0 {
			for i := limit; i > limit/2; i-- {
				if unicode.IsSpace(runes[i]) {
					cut = i
					break
				}
			}
		}
		if cut < 0 {
			cut = limit
		}
		out = append(out, strings.TrimSpace(string(runes[:cut])))
		s = strings.TrimSpace(string(runes[cut:]))
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
