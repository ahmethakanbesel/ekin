package caption_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"kurgu/pkg/caption"
)

// say times words one after another, 0.4 s each with 0.1 s between.
func say(start float64, text string) []caption.Word {
	var out []caption.Word
	for _, f := range strings.Fields(text) {
		out = append(out, caption.Word{Text: f, Start: start, End: start + 0.4})
		start += 0.5
	}
	return out
}

func TestCues(t *testing.T) {
	long := say(0, "Kaygı bozukluğu yaşayan birçok kişi belirtilerini uzun süre fark etmeden günlük hayatına devam ediyor ve yardım almayı erteliyor")
	tests := []struct {
		name  string
		words []caption.Word
		want  []caption.Cue
	}{
		{
			name:  "a sentence ends a cue",
			words: append(say(0, "Merhaba dünya."), say(1.0, "Nasılsın?")...),
			want: []caption.Cue{
				{Start: 0, End: 1.0, Lines: []string{"Merhaba dünya."}},
				{Start: 1.0, End: 1.9, Lines: []string{"Nasılsın?"}},
			},
		},
		{
			name:  "a long pause ends a cue",
			words: append(say(0, "önce bu"), say(3, "sonra şu")...),
			want: []caption.Cue{
				{Start: 0, End: 1.4, Lines: []string{"önce bu"}},
				{Start: 3, End: 4.4, Lines: []string{"sonra şu"}},
			},
		},
		{
			name:  "a short cue stays on screen a while unless the next starts",
			words: []caption.Word{{Text: "Evet.", Start: 0, End: 0.1}, {Text: "Hayır.", Start: 0.3, End: 0.4}},
			want: []caption.Cue{
				{Start: 0, End: 0.3, Lines: []string{"Evet."}},
				{Start: 0.3, End: 1.1, Lines: []string{"Hayır."}},
			},
		},
		{
			name:  "empty and invalid words are skipped",
			words: []caption.Word{{Text: " ", Start: 0, End: 1}, {Text: "bir", Start: math.NaN(), End: 1}, {Text: "iki", Start: 2, End: 1}, {Text: "üç", Start: 1, End: 1.5}},
			want:  []caption.Cue{{Start: 1, End: 2.0, Lines: []string{"üç"}}},
		},
		{name: "no words", words: nil, want: []caption.Cue{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := caption.Cues(tt.words)
			if len(got) != len(tt.want) {
				t.Fatalf("Cues() = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if !reflect.DeepEqual(got[i].Lines, tt.want[i].Lines) || !near(got[i].Start, tt.want[i].Start) || !near(got[i].End, tt.want[i].End) {
					t.Errorf("cue %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}

	t.Run("long speech splits into cues of at most two lines and six seconds", func(t *testing.T) {
		got := caption.Cues(long)
		if len(got) < 2 {
			t.Fatalf("Cues() = %d cues, want several", len(got))
		}
		var words []string
		for _, c := range got {
			if len(c.Lines) > 2 {
				t.Errorf("cue %+v has %d lines", c, len(c.Lines))
			}
			for _, l := range c.Lines {
				if n := len([]rune(l)); n > 42 {
					t.Errorf("line %q has %d characters", l, n)
				}
				words = append(words, strings.Fields(l)...)
			}
			if c.End-c.Start > 6.5 {
				t.Errorf("cue %+v lasts %.1f s", c, c.End-c.Start)
			}
		}
		if len(words) != len(long) {
			t.Errorf("cues hold %d words, want %d", len(words), len(long))
		}
		for i := 1; i < len(got); i++ {
			if got[i].Start < got[i-1].End {
				t.Errorf("cue %d starts at %.2f before the previous ends at %.2f", i, got[i].Start, got[i-1].End)
			}
		}
	})
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSRT(t *testing.T) {
	cues := []caption.Cue{
		{Start: 0, End: 1.5, Lines: []string{"Merhaba"}},
		{Start: 3725.0425, End: 3726, Lines: []string{"iki", "satır"}},
	}
	want := "1\n00:00:00,000 --> 00:00:01,500\nMerhaba\n\n" +
		"2\n01:02:05,043 --> 01:02:06,000\niki\nsatır\n\n"
	if got := string(caption.SRT(cues)); got != want {
		t.Errorf("SRT() =\n%q\nwant\n%q", got, want)
	}
}

func TestVTT(t *testing.T) {
	cues := []caption.Cue{{Start: 0.25, End: 2, Lines: []string{"a < b & c > d"}}}
	want := "WEBVTT\n\n00:00:00.250 --> 00:00:02.000\na &lt; b &amp; c &gt; d\n\n"
	if got := string(caption.VTT(cues)); got != want {
		t.Errorf("VTT() =\n%q\nwant\n%q", got, want)
	}
}
