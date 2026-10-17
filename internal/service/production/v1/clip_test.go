package production

import (
	"encoding/json"
	"testing"
	"time"

	storydomain "kurgu/internal/domain/story/v1"
)

func TestSliceTimings(t *testing.T) {
	raw := []byte(`{"engine":"kokoro","sampleRate":24000,"duration":30,
		"words":[{"text":"a","start":0,"end":1},{"text":"b","start":4.9,"end":5.6},{"text":"c","start":10,"end":11}],
		"segments":[{"text":"A.","start":0,"end":4.8},{"text":"B c.","start":4.9,"end":11.2},{"text":"D.","start":11.5,"end":15}]}`)
	ms := func(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
	tests := []struct {
		name       string
		start, end float64
		want       []string // sentences kept
		wantErr    bool
	}{
		{"one sentence", 4.9, 11.2, []string{"B c."}, false},
		{"two sentences", 4.9, 15, []string{"B c.", "D."}, false},
		{"rounding slack", 4.91, 11.19, []string{"B c."}, false},
		{"no whole sentence", 6, 11.2, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := storydomain.NewClip(ms(tt.start), ms(tt.end))
			out, sentences, err := sliceTimings(raw, c)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if err != nil {
				return
			}
			var got []string
			for _, s := range sentences {
				got = append(got, s.Text)
			}
			if len(got) != len(tt.want) || got[0] != tt.want[0] {
				t.Fatalf("sentences = %v, want %v", got, tt.want)
			}
			var back struct {
				Engine     string  `json:"engine"`
				SampleRate int     `json:"sampleRate"`
				Duration   float64 `json:"duration"`
				Words      []struct {
					Text       string
					Start, End float64
				} `json:"words"`
			}
			if err := json.Unmarshal(out, &back); err != nil {
				t.Fatal(err)
			}
			if back.Engine != "kokoro" || back.SampleRate != 24000 || back.Words[0].Text != "b" || back.Words[0].Start != 0 {
				t.Fatalf("timings = %+v", back)
			}
			if want := float64(int((tt.end-tt.start)*1000+0.5)) / 1000; back.Duration != want {
				t.Fatalf("duration = %v, want %v", back.Duration, want)
			}
		})
	}
}
