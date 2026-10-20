package agenttool_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/render"
	"github.com/ahmethakanbesel/ekin/internal/agenttool"
)

// prober stands in for Chrome: every composition loads with meta.
type prober struct{ meta render.Meta }

func (p prober) Probe(context.Context, string, string) (render.Meta, []string, error) {
	return p.meta, nil, nil
}

func TestTTSRefusedForAClip(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "narration"), 0o750)
	_ = os.WriteFile(filepath.Join(dir, "narration", "narration.txt"), []byte("Bir cümle."), 0o600)
	var out bytes.Buffer
	env := agenttool.Env{Dir: dir, Stdout: &out, Stderr: &out, Spec: agenttool.Spec{Language: "tr", Clip: true}}
	err := agenttool.Run(context.Background(), env, []string{"tts"})
	if err == nil || !strings.Contains(err.Error(), "long video's narration") {
		t.Fatalf("tts for a clip = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "narration", "speech.wav")); err == nil {
		t.Fatal("tts wrote narration for a clip")
	}
}

func TestCheckKeepsAClipsNarration(t *testing.T) {
	tests := []struct {
		name      string
		narration string // timings.json duration
		wantErr   string
	}{
		{"as prepared", "20.4", ""},
		{"within rounding", "20.45", ""},
		{"changed", "15", "not the clip's 20.40 s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			_ = os.MkdirAll(filepath.Join(dir, "narration"), 0o750)
			_ = os.WriteFile(filepath.Join(dir, "composition.html"), []byte(`<script src="/kit/ekin.js"></script>`), 0o600)
			_ = os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"title":"Öfke","description":"d","tags":[]}`), 0o600)
			_ = os.WriteFile(filepath.Join(dir, "narration", "timings.json"), []byte(`{"duration": `+tt.narration+`, "words": []}`), 0o600)
			spec := agenttool.Spec{Language: "tr", Width: 1080, Height: 1920, MinDuration: 20.4, MaxDuration: 24.4, Clip: true}
			rep := agenttool.Check(context.Background(), prober{render.Meta{Width: 1080, Height: 1920, FPS: 30, Duration: 21}}, dir, "composition.html", spec)
			errs := strings.Join(rep.Errors, "\n")
			if tt.wantErr == "" && !rep.OK {
				t.Fatalf("check failed: %s", errs)
			}
			if tt.wantErr != "" && (rep.OK || !strings.Contains(errs, tt.wantErr)) {
				t.Fatalf("errors = %s, want %q", errs, tt.wantErr)
			}
		})
	}
}

func TestSliceTimings(t *testing.T) {
	raw := []byte(`{"engine":"kokoro","sampleRate":24000,"duration":30,
		"words":[{"text":"a","start":0,"end":1},{"text":"b","start":4.9,"end":5.6},{"text":"c","start":10,"end":11}],
		"segments":[{"text":"A.","start":0,"end":4.8},{"text":"B c.","start":4.9,"end":11.2},{"text":"D.","start":11.5,"end":15}]}`)
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
			out, sentences, err := agenttool.SliceTimings(raw, tt.start, tt.end)
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
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
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
			if want := math.Round((tt.end-tt.start)*1000) / 1000; back.Duration != want {
				t.Fatalf("duration = %v, want %v", back.Duration, want)
			}
		})
	}
}

// clipDir is a short's work dir with a 60 s window of 5 s sentences
// (each 4.6 s, then a 0.4 s pause) and a cut on sentences 3–8.
func clipDir(t *testing.T, earliest, latest float64) string {
	t.Helper()
	dir := t.TempDir()
	var segs []map[string]any
	for i := range 12 {
		s := float64(i) * 5
		segs = append(segs, map[string]any{"text": fmt.Sprintf("S%d.", i+1), "start": s, "end": s + 4.6})
	}
	window, _ := json.Marshal(map[string]any{"engine": "okur", "duration": 60, "words": []any{}, "segments": segs})
	_ = os.MkdirAll(filepath.Join(dir, "clip"), 0o750)
	_ = os.WriteFile(filepath.Join(dir, agenttool.ClipWindowJSON), window, 0o600)
	_ = os.WriteFile(filepath.Join(dir, agenttool.ClipWindowWAV), wavBytes(60), 0o600)
	c, _ := json.Marshal(agenttool.Clip{WindowStart: 85, Start: 10, End: 39.6, MinLength: 20, MaxLength: 60, Earliest: earliest, Latest: latest})
	_ = os.WriteFile(filepath.Join(dir, agenttool.ClipFile), c, 0o600)
	if _, err := agenttool.CutNarration(filepath.Join(dir, agenttool.ClipWindowWAV), window, 10, 39.6, dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// wavBytes is a mono 16-bit WAV of seconds at 1000 frames a second.
func wavBytes(seconds int) []byte {
	data := make([]byte, seconds*2000)
	h := []byte("RIFF")
	h = binary.LittleEndian.AppendUint32(h, uint32(36+len(data)))
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint32(h, 1000)
	h = binary.LittleEndian.AppendUint32(h, 2000)
	h = binary.LittleEndian.AppendUint16(h, 2)
	h = binary.LittleEndian.AppendUint16(h, 16)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	return append(h, data...)
}

func TestClipToolMovesTheCut(t *testing.T) {
	tests := []struct {
		name            string
		first, last     int
		earliest        float64
		wantErr         string
		wantStart, want float64 // the cut in the window afterwards
	}{
		{"a sentence earlier", 2, 8, 0, "", 5, 39.6},
		{"one more at the end", 3, 9, 0, "", 10, 44.6},
		{"too short", 3, 5, 0, "must last 20–60 s", 10, 39.6},
		{"past the window", 3, 13, 0, "from 1 to 12", 10, 39.6},
		{"backwards", 8, 3, 0, "first ≤ last", 10, 39.6},
		{"too close to another short", 2, 8, 8, "too close to another short", 10, 39.6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := clipDir(t, tt.earliest, 60)
			var out bytes.Buffer
			env := agenttool.Env{Dir: dir, Stdout: &out, Stderr: &out, Spec: agenttool.Spec{Clip: true}}
			err := agenttool.Run(context.Background(), env, []string{"clip", "--first", fmt.Sprint(tt.first), "--last", fmt.Sprint(tt.last)})
			if tt.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			c, _, _ := agenttool.ReadClip(dir)
			if c.Start != tt.wantStart || c.End != tt.want || c.WindowStart != 85 {
				t.Fatalf("clip = %+v", c)
			}
			var timings struct {
				Duration float64 `json:"duration"`
			}
			raw, _ := os.ReadFile(filepath.Join(dir, "narration", "timings.json"))
			_ = json.Unmarshal(raw, &timings)
			if math.Abs(timings.Duration-(tt.want-tt.wantStart)) > 0.001 {
				t.Fatalf("narration lasts %.3f s, want %.3f", timings.Duration, tt.want-tt.wantStart)
			}
			text, _ := os.ReadFile(filepath.Join(dir, "narration", "narration.txt"))
			if want := fmt.Sprintf("S%d.", int(tt.wantStart/5)+1); !strings.HasPrefix(string(text), want) {
				t.Fatalf("narration.txt = %q, want it to start with %q", text, want)
			}
		})
	}
}

func TestClipToolOnlyForAClip(t *testing.T) {
	dir := clipDir(t, 0, 60)
	var out bytes.Buffer
	err := agenttool.Run(context.Background(), agenttool.Env{Dir: dir, Stdout: &out, Stderr: &out}, []string{"clip", "--first", "2", "--last", "8"})
	if err == nil || !strings.Contains(err.Error(), "only a short cut") {
		t.Fatalf("clip outside clip mode = %v", err)
	}
}

func TestCheckFollowsAMovedCut(t *testing.T) {
	dir := clipDir(t, 0, 60)
	var out bytes.Buffer
	env := agenttool.Env{Dir: dir, Stdout: &out, Stderr: &out, Spec: agenttool.Spec{Clip: true}}
	if err := agenttool.Run(context.Background(), env, []string{"clip", "--first", "2", "--last", "8"}); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "composition.html"), []byte(`<script src="/kit/ekin.js"></script>`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"title":"Öfke","description":"d","tags":[]}`), 0o600)
	// The spec still holds the length prepared (29.6 s); the cut now
	// lasts 34.6 s and the composition follows it.
	spec := agenttool.Spec{Language: "tr", Width: 1080, Height: 1920, MinDuration: 29.6, MaxDuration: 33.6, Clip: true}
	rep := agenttool.Check(context.Background(), prober{render.Meta{Width: 1080, Height: 1920, FPS: 30, Duration: 35}}, dir, "composition.html", spec)
	if !rep.OK {
		t.Fatalf("check of a moved cut failed: %v", rep.Errors)
	}
}
