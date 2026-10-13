// Package speech synthesises narration with word timings by running the
// scripts in tools/speech: okur-tts for Turkish, Kokoro for English.
package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Word is one written token with its spoken interval, in seconds.
type Word struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Segment is one synthesised sentence or paragraph.
type Segment struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Timings is the scripts' timings.json.
type Timings struct {
	Engine     string    `json:"engine"`
	SampleRate int       `json:"sampleRate"`
	Duration   float64   `json:"duration"`
	Words      []Word    `json:"words"`
	Segments   []Segment `json:"segments"`
}

// Synth runs the scripts.
type Synth struct {
	UV        string // uv binary; default "uv"
	ScriptDir string // directory holding okur_say.py and kokoro_say.py
	ModelDir  string // optional weights cache root
	Threads   int    // ONNX Runtime threads; 0 = all cores
}

// Say synthesises the text in textFile into outDir/speech.wav and
// outDir/timings.json. lang is "tr" or "en".
func (s *Synth) Say(ctx context.Context, lang, textFile, outDir, voice string, speed float64) (Timings, error) {
	script := ""
	switch lang {
	case "tr":
		script = "okur_say.py"
	case "en":
		script = "kokoro_say.py"
	default:
		return Timings{}, fmt.Errorf("speech: no voice for language %q", lang)
	}
	uv := s.UV
	if uv == "" {
		uv = "uv"
	}
	if speed == 0 {
		speed = 1
	}
	args := []string{"run", "--quiet", "--script", filepath.Join(s.ScriptDir, script),
		"--text-file", textFile, "--out-dir", outDir, "--speed", strconv.FormatFloat(speed, 'f', 2, 64)}
	if voice != "" {
		args = append(args, "--voice", voice)
	}
	if s.ModelDir != "" {
		args = append(args, "--model-dir", filepath.Join(s.ModelDir, strings.TrimSuffix(script, "_say.py")))
	}
	cmd := exec.CommandContext(ctx, uv, args...) //nolint:gosec // fixed scripts, arguments built here
	cmd.Env = os.Environ()
	if s.Threads > 0 {
		cmd.Env = append(cmd.Env, "KURGU_SPEECH_THREADS="+strconv.Itoa(s.Threads))
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Timings{}, errors.New("speech: uv is not installed")
		}
		return Timings{}, fmt.Errorf("speech: %s failed: %w: %s", script, err, lastLines(stderr.String(), 5))
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "timings.json")) //nolint:gosec // our own output dir
	if err != nil {
		return Timings{}, fmt.Errorf("speech: read timings: %w", err)
	}
	var t Timings
	if err := json.Unmarshal(raw, &t); err != nil {
		return Timings{}, fmt.Errorf("speech: parse timings: %w", err)
	}
	if t.Duration <= 0 || len(t.Words) == 0 {
		return Timings{}, errors.New("speech: the script produced no speech")
	}
	return t, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
