package agenttool

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kurgu/pkg/wav"
)

// A short cut from a long video speaks a slice of the long video's
// narration. Its work dir holds the slice in narration/ (as tts would
// write it) and, under clip/, a window of the long narration around it
// so `kurgu tool clip` can move the cut by whole sentences.

// Clip files in the work dir.
const (
	ClipFile       = "clip/clip.json"
	ClipWindowWAV  = "clip/window.wav" //nolint:gosec // a file name ("clip window" reads as "pw" to G101)
	ClipWindowJSON = "clip/window.json"
)

// ClipFade is how long a narration slice fades in and out.
const ClipFade = 15 * time.Millisecond

// ClipTail is how far a short's composition may run past its
// narration, for an end card.
const ClipTail = 4.0

// Clip is clip/clip.json: where the short's narration sits in the
// window, and where the window sits in the long video, in seconds.
type Clip struct {
	WindowStart float64 `json:"windowStart"` // in the long narration
	Start       float64 `json:"start"`       // in the window
	End         float64 `json:"end"`         // in the window
	MinLength   float64 `json:"minLength"`
	MaxLength   float64 `json:"maxLength"`
	// Earliest and Latest bound the cut in the window, keeping the gap
	// to the long video's other shorts.
	Earliest float64 `json:"earliest"`
	Latest   float64 `json:"latest"`
}

// Length is the short's narration length.
func (c Clip) Length() float64 { return c.End - c.Start }

// ReadClip reads clip/clip.json in dir; ok is false when there is none.
func ReadClip(dir string) (Clip, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ClipFile)) //nolint:gosec // the agent's work dir
	if os.IsNotExist(err) {
		return Clip{}, false, nil
	}
	if err != nil {
		return Clip{}, false, err
	}
	var c Clip
	if err := json.Unmarshal(raw, &c); err != nil {
		return Clip{}, false, fmt.Errorf("%s: %w", ClipFile, err)
	}
	return c, true, nil
}

// Sentence is one timed sentence of a narration, in seconds.
type Sentence struct {
	Text       string
	Start, End float64
}

// SliceTimings keeps the words and sentences of a narration's
// timings.json that fall within start–end seconds, shifted so start is
// 0, and sets the duration to the span's. Everything else in the file
// (engine, voice) is kept as it is.
func SliceTimings(raw []byte, start, end float64) ([]byte, []Sentence, error) {
	var t map[string]json.RawMessage
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, nil, fmt.Errorf("narration timings are unreadable: %w", err)
	}
	const slack = 0.02 // snapping and millisecond rounding
	within := func(a, b float64) bool { return a >= start-slack && b <= end+slack }
	shift := func(v float64) float64 { return math.Max(0, math.Round((v-start)*1000)/1000) }
	slice := func(name string) ([]map[string]any, error) {
		var items []map[string]any
		if r, ok := t[name]; ok {
			if err := json.Unmarshal(r, &items); err != nil {
				return nil, fmt.Errorf("narration timings are unreadable: %w", err)
			}
		}
		kept := []map[string]any{}
		for _, it := range items {
			a, _ := it["start"].(float64)
			b, _ := it["end"].(float64)
			if within(a, b) {
				it["start"], it["end"] = shift(a), math.Min(shift(b), end-start)
				kept = append(kept, it)
			}
		}
		return kept, nil
	}
	words, err := slice("words")
	if err != nil {
		return nil, nil, err
	}
	segments, err := slice("segments")
	if err != nil {
		return nil, nil, err
	}
	if len(segments) == 0 {
		return nil, nil, errors.New("the span holds no whole sentence of the narration")
	}
	sentences := make([]Sentence, 0, len(segments))
	for _, seg := range segments {
		text, _ := seg["text"].(string)
		a, _ := seg["start"].(float64)
		b, _ := seg["end"].(float64)
		sentences = append(sentences, Sentence{Text: text, Start: a, End: b})
	}
	set := func(name string, v any) {
		b, _ := json.Marshal(v)
		t[name] = b
	}
	set("words", words)
	set("segments", segments)
	set("duration", math.Round((end-start)*1000)/1000)
	b, err := json.MarshalIndent(t, "", "  ")
	return b, sentences, err
}

// CutNarration writes start–end seconds of a narration (the WAV at
// wavIn with its timings) as the short's narration/ in dir: speech.wav,
// timings.json and narration.txt. It returns the sentences kept.
func CutNarration(wavIn string, timings []byte, start, end float64, dir string) ([]Sentence, error) {
	sliced, sentences, err := SliceTimings(timings, start, end)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "narration"), 0o750); err != nil {
		return nil, err
	}
	in, err := os.Open(wavIn) //nolint:gosec // the work dir or our temp dir
	if err != nil {
		return nil, errors.New("the long video's narration audio is missing")
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(filepath.Join(dir, "narration", "speech.wav")) //nolint:gosec // the work dir
	if err != nil {
		return nil, err
	}
	sec := func(s float64) time.Duration { return time.Duration(math.Round(s*1000)) * time.Millisecond }
	if _, err := wav.Cut(in, out, sec(start), sec(end), ClipFade); err != nil {
		_ = out.Close()
		return nil, fmt.Errorf("cut the narration: %w", err)
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "narration", "timings.json"), sliced, 0o600); err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(sentences))
	for _, s := range sentences {
		texts = append(texts, s.Text)
	}
	return sentences, os.WriteFile(filepath.Join(dir, "narration", "narration.txt"), []byte(strings.Join(texts, " ")+"\n"), 0o600)
}

// WindowSentences reads the numbered sentences of the clip's window.
func WindowSentences(dir string) ([]Sentence, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ClipWindowJSON)) //nolint:gosec // the agent's work dir
	if err != nil {
		return nil, fmt.Errorf("%s is missing: %w", ClipWindowJSON, err)
	}
	var t struct {
		Segments []struct {
			Text  string  `json:"text"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("%s: %w", ClipWindowJSON, err)
	}
	out := make([]Sentence, 0, len(t.Segments))
	for _, s := range t.Segments {
		out = append(out, Sentence{Text: s.Text, Start: s.Start, End: s.End})
	}
	return out, nil
}

// clip moves the short's cut to sentences first–last (1-based) of the
// window and rewrites its narration.
func (e Env) clip(first, last int) error {
	if !e.Spec.Clip {
		return errors.New("only a short cut from a long video has a clip to move")
	}
	c, ok, err := ReadClip(e.Dir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is missing", ClipFile)
	}
	sentences, err := WindowSentences(e.Dir)
	if err != nil {
		return err
	}
	if first < 1 || last < first || last > len(sentences) {
		return fmt.Errorf("--first and --last must be sentence numbers from 1 to %d with first ≤ last (see sources/clip.md)", len(sentences))
	}
	start, end := sentences[first-1].Start, sentences[last-1].End
	if l := end - start; l < c.MinLength-0.001 || l > c.MaxLength+0.001 {
		return fmt.Errorf("sentences %d–%d last %.1f s; a short must last %.0f–%.0f s", first, last, l, c.MinLength, c.MaxLength)
	}
	if start < c.Earliest-0.001 || end > c.Latest+0.001 {
		return fmt.Errorf("sentences %d–%d come too close to another short of the long video; stay within %.2f–%.2f s of the window",
			first, last, c.Earliest, c.Latest)
	}
	window, err := os.ReadFile(e.path(ClipWindowJSON))
	if err != nil {
		return err
	}
	kept, err := CutNarration(e.path(ClipWindowWAV), window, start, end, e.Dir)
	if err != nil {
		return err
	}
	c.Start, c.End = start, end
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(e.path(ClipFile), b, 0o600); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(e.Stdout, "the short now speaks sentences %d–%d (%d sentences, %.2f s); narration/ is rewritten.\n"+
		"Re-time composition.html to narration/timings.json, update script.md and run %s check.\n",
		first, last, len(kept), end-start, toolName)
	return nil
}

const toolName = "kurgu tool"

// clipSpec returns spec with the clip's current length as its duration
// bounds, when dir holds a short whose cut may have moved.
func clipSpec(dir string, spec Spec) (Spec, error) {
	if !spec.Clip {
		return spec, nil
	}
	c, ok, err := ReadClip(dir)
	if err != nil || !ok {
		return spec, err
	}
	spec.MinDuration, spec.MaxDuration = c.Length(), c.Length()+ClipTail
	return spec, nil
}
