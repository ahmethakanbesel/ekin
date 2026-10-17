// Package caption turns narration word timings into subtitle cues and
// writes them as SubRip (.srt) or WebVTT (.vtt), the files YouTube and
// players accept as a caption track.
package caption

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Word is one spoken word and when it is said, in seconds.
type Word struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Cue is one caption on screen: up to two lines from Start to End.
type Cue struct {
	Start, End float64
	Lines      []string
}

// Limits of a cue, after common broadcast guidelines.
const (
	lineChars   = 42  // characters per line
	maxLines    = 2   // lines per cue
	maxDuration = 6.0 // seconds on screen
	pauseBreak  = 1.0 // a silence this long starts a new cue
	hold        = 0.5 // seconds a cue stays after its last word, until the next starts
	minDuration = 0.8 // seconds a cue is shown at least, until the next starts
)

// Cues groups words into cues. A cue ends after a sentence, before a
// long pause, or when the next word would not fit its lines or time.
// Words without text or with an invalid time are skipped.
func Cues(words []Word) []Cue {
	var (
		groups [][]Word
		cur    []Word
	)
	for _, w := range words {
		w.Text = strings.TrimSpace(w.Text)
		if w.Text == "" || math.IsNaN(w.Start) || math.IsNaN(w.End) || math.IsInf(w.Start, 0) || math.IsInf(w.End, 0) || w.End < w.Start {
			continue
		}
		if len(cur) > 0 && !fits(cur, w) {
			groups = append(groups, cur)
			cur = nil
		}
		cur = append(cur, w)
		if endsSentence(w.Text) {
			groups = append(groups, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	cues := make([]Cue, 0, len(groups))
	for i, g := range groups {
		start, end := g[0].Start, g[len(g)-1].End
		next := math.Inf(1)
		if i+1 < len(groups) {
			next = groups[i+1][0].Start
		}
		end = math.Min(math.Max(end+hold, start+minDuration), math.Max(next, end))
		cues = append(cues, Cue{Start: start, End: end, Lines: wrap(texts(g))})
	}
	return cues
}

// fits reports whether w can join the cue cur.
func fits(cur []Word, w Word) bool {
	last := cur[len(cur)-1]
	if w.Start-last.End >= pauseBreak || w.End-cur[0].Start > maxDuration {
		return false
	}
	return len(wrap(append(texts(cur), w.Text))) <= maxLines
}

func endsSentence(s string) bool {
	s = strings.TrimRight(s, `"'”’»)]`)
	return strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") || strings.HasSuffix(s, "…")
}

func texts(ws []Word) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Text
	}
	return out
}

// wrap breaks words into lines of at most lineChars (a longer word gets
// a line of its own), balancing two lines so neither is much shorter.
func wrap(words []string) []string {
	one := strings.Join(words, " ")
	if utf8.RuneCountInString(one) <= lineChars {
		return []string{one}
	}
	// Two lines: the split whose longer line is shortest.
	best, bestLen := -1, math.MaxInt
	for i := 1; i < len(words); i++ {
		a, b := utf8.RuneCountInString(strings.Join(words[:i], " ")), utf8.RuneCountInString(strings.Join(words[i:], " "))
		if a > lineChars || b > lineChars {
			continue
		}
		if l := max(a, b); l < bestLen {
			best, bestLen = i, l
		}
	}
	if best > 0 {
		return []string{strings.Join(words[:best], " "), strings.Join(words[best:], " ")}
	}
	// Greedy, which may take more than two lines.
	var lines []string
	line := ""
	for _, w := range words {
		switch {
		case line == "":
			line = w
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) <= lineChars:
			line += " " + w
		default:
			lines = append(lines, line)
			line = w
		}
	}
	return append(lines, line)
}

// SRT writes cues as SubRip.
func SRT(cues []Cue) []byte {
	var b strings.Builder
	for i, c := range cues {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, stamp(c.Start, ","), stamp(c.End, ","), strings.Join(c.Lines, "\n"))
	}
	return []byte(b.String())
}

// VTT writes cues as WebVTT.
func VTT(cues []Cue) []byte {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, c := range cues {
		lines := make([]string, len(c.Lines))
		for i, l := range c.Lines {
			lines[i] = vttEscape.Replace(l)
		}
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n", stamp(c.Start, "."), stamp(c.End, "."), strings.Join(lines, "\n"))
	}
	return []byte(b.String())
}

// vttEscape escapes what WebVTT cue text reads as markup; "-->" cannot
// appear in a cue either.
var vttEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// stamp formats seconds as hh:mm:ss,mmm (SRT) or hh:mm:ss.mmm (VTT).
func stamp(s float64, sep string) string {
	ms := int64(math.Round(math.Max(s, 0) * 1000))
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, sep, ms%1000)
}
