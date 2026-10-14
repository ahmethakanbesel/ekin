package agenttool

import (
	"strings"
	"testing"

	"kurgu/internal/adapter/render"
)

// frame is a 20×40 dark sample with a light box drawn from row y0 to y1
// at column x (a box of 4 columns), or none when y1 <= y0.
func frame(t float64, x, y0, y1 int) render.Sample {
	s := render.Sample{T: t, W: 20, H: 40, Gray: make([]uint8, 800)}
	for i := range s.Gray {
		s.Gray[i] = 20
	}
	for y := y0; y < y1; y++ {
		for c := x; c < x+4; c++ {
			s.Gray[y*20+c] = 220
		}
	}
	return s
}

// video samples every half second until end, drawing each with draw.
func video(end float64, draw func(t float64) render.Sample) []render.Sample {
	var out []render.Sample
	for t := 0.0; t < end; t += sampleEvery {
		out = append(out, draw(t))
	}
	return out
}

// moving fills the visual area (rows 4–26 of 40) and shifts every second,
// never into columns 16–19.
func moving(t float64) render.Sample { return frame(t, int(t)%12, 4, 26) }

func TestCheckPicture(t *testing.T) {
	tests := []struct {
		name      string
		samples   []render.Sample
		narration float64
		long      bool
		errs      []string // substrings, in order
		warns     []string
	}{
		{name: "a busy, full frame passes", samples: video(10, moving), narration: 10},
		{
			name: "captions alone on an empty frame",
			samples: video(10, func(t float64) render.Sample {
				if t >= 4 && t < 5.5 {
					return frame(t, 0, 0, 0)
				}
				return moving(t)
			}),
			narration: 10,
			errs:      []string{"from 4.0 to 5.5 s the frame is empty"},
		},
		{
			name: "a blank half second is a cut, not an empty beat",
			samples: video(10, func(t float64) render.Sample {
				if t == 4 {
					return frame(t, 0, 0, 0)
				}
				return moving(t)
			}),
			narration: 10,
		},
		{
			name: "a still picture for 5 s",
			samples: video(12, func(t float64) render.Sample {
				if t >= 3 && t < 8 {
					return frame(t, 16, 4, 26)
				}
				return moving(t)
			}),
			narration: 12,
			errs:      []string{"from 3.0 to 8.0 s nothing on screen changes"},
		},
		{
			name: "long videos may hold a picture longer",
			samples: video(12, func(t float64) render.Sample {
				if t >= 3 && t < 8 {
					return frame(t, 16, 4, 26)
				}
				return moving(t)
			}),
			narration: 12,
			long:      true,
		},
		{
			name:      "the tail after the narration may hold a closing card",
			samples:   append(video(10, moving), frame(10, 0, 0, 0), frame(10.5, 0, 0, 0), frame(11, 0, 0, 0)),
			narration: 10,
		},
		{
			name:      "visuals crammed into the top leave the lower part empty",
			samples:   video(10, func(t float64) render.Sample { return frame(t, int(t)%16, 2, 14) }),
			narration: 10,
			warns:     []string{"the lower part of the frame (50–64% of the height) is empty in 100% of frames"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, warns := checkPicture(tt.samples, tt.narration, tt.long)
			match(t, "errors", errs, tt.errs)
			match(t, "warnings", warns, tt.warns)
		})
	}
}

func match(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %q, want %d matching %q", what, got, len(want), want)
	}
	for i := range want {
		if !strings.Contains(got[i], want[i]) {
			t.Fatalf("%s[%d] = %q, want it to contain %q", what, i, got[i], want[i])
		}
	}
}
