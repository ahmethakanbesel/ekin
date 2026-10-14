package agenttool

import (
	"context"
	"fmt"
	"slices"

	"kurgu/internal/adapter/render"
)

// Sampler renders small frames with the captions hidden
// (render.Renderer). Check uses it, when the prober has it, to judge the
// picture itself: captions over an empty frame are not a visual.
type Sampler interface {
	Sample(ctx context.Context, dir, pagePath string, every float64) ([]render.Sample, []string, error)
}

// The picture limits. A pixel is ink when its luminance is more than
// inkDelta from the frame's median (the background); a frame is empty
// below emptyInk. Two samples differ when more than changeShare of their
// pixels moved by inkDelta. Package variables, not constants: gosec 2.23
// panics on float constants in integer conversions.
var (
	sampleEvery  = 0.5
	inkDelta     = 24
	emptyInk     = 0.01
	changeShare  = 0.003
	maxEmpty     = 1.0 // seconds of caption-only picture
	maxStill     = 4.0 // seconds without a visual change, short videos
	maxStillLong = 8.0
	lowerBand    = [2]float64{0.5, 0.64} // of the height: the visual area's lower part
	lowerUsed    = 0.015                 // ink share that counts as using the band
	minLowerUse  = 0.6                   // share of frames that should use it
)

// checkPicture reports stretches where the frame shows nothing but the
// captions, stretches where nothing on screen changes, and (vertical
// video) a lower half left empty. Only the narrated part counts: the
// tail may hold a closing card.
func checkPicture(samples []render.Sample, narration float64, long bool) (errs, warns []string) {
	if narration > 0 {
		samples = slices.DeleteFunc(slices.Clone(samples), func(s render.Sample) bool { return s.T > narration })
	}
	if len(samples) == 0 {
		return nil, nil
	}
	ink := make([]float64, len(samples))
	for i, s := range samples {
		ink[i] = inkShare(s, 0, s.H)
	}

	for _, r := range runs(samples, func(i int) bool { return ink[i] < emptyInk }) {
		if r[1]-r[0] >= maxEmpty {
			errs = append(errs, fmt.Sprintf("from %.1f to %.1f s the frame is empty apart from the captions; "+
				"keep a visual for every line on screen", r[0], r[1]))
		}
	}

	still := maxStill
	if long {
		still = maxStillLong
	}
	for _, r := range runs(samples, func(i int) bool { return i > 0 && !changed(samples[i-1], samples[i]) }) {
		// The run starts at the last sample that changed.
		if from := r[0] - sampleEvery; r[1]-from >= still {
			errs = append(errs, fmt.Sprintf("from %.1f to %.1f s nothing on screen changes; "+
				"reveal, highlight, move or swap something every 2–3 s", from, r[1]))
		}
	}

	s0 := samples[0]
	if !long && s0.H > s0.W {
		used := 0
		lo, hi := int(lowerBand[0]*float64(s0.H)), int(lowerBand[1]*float64(s0.H))
		for _, s := range samples {
			if inkShare(s, lo, hi) >= lowerUsed {
				used++
			}
		}
		if share := float64(used) / float64(len(samples)); share < minLowerUse {
			warns = append(warns, fmt.Sprintf("the lower part of the frame (%.0f–%.0f%% of the height) is empty in %.0f%% of frames; "+
				"make the visuals larger or place them lower, keeping only the bottom 18%% clear",
				lowerBand[0]*100, lowerBand[1]*100, (1-share)*100))
		}
	}
	return errs, warns
}

// runs returns [start, end] times of the maximal runs of samples where
// in(i) holds; end is where the run's last sample's interval ends.
func runs(samples []render.Sample, in func(i int) bool) [][2]float64 {
	var out [][2]float64
	start := -1
	for i := range samples {
		if in(i) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, [2]float64{samples[start].T, samples[i].T})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, [2]float64{samples[start].T, samples[len(samples)-1].T + sampleEvery})
	}
	return out
}

// inkShare is the share of pixels in rows [y0, y1) that stand out from
// the frame's background (its median luminance).
func inkShare(s render.Sample, y0, y1 int) float64 {
	var hist [256]int
	for _, g := range s.Gray {
		hist[g]++
	}
	median, seen := 0, 0
	for v, n := range hist {
		if seen += n; seen*2 >= len(s.Gray) {
			median = v
			break
		}
	}
	px := s.Gray[y0*s.W : y1*s.W]
	n := 0
	for _, g := range px {
		if d := int(g) - median; d > inkDelta || d < -inkDelta {
			n++
		}
	}
	return float64(n) / float64(max(1, len(px)))
}

// changed reports whether a visible share of the picture moved between
// two samples.
func changed(a, b render.Sample) bool {
	if len(a.Gray) != len(b.Gray) {
		return true
	}
	n := 0
	for i := range a.Gray {
		if d := int(a.Gray[i]) - int(b.Gray[i]); d > inkDelta || d < -inkDelta {
			n++
		}
	}
	return float64(n)/float64(len(a.Gray)) > changeShare
}
