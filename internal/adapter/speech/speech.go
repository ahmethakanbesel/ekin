// Package speech synthesises narration with word timings: okur-tts or
// EMA Lightning for Turkish and Kokoro for English by running the scripts
// in tools/speech, or ElevenLabs for either over its API.
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
	Engine string `json:"engine"`
	// Voice and Model are what spoke: the voice ID ("" for the engine's
	// only voice) and the ElevenLabs model. Ekin adds them after the
	// script runs, so the file says who read the narration.
	Voice      string    `json:"voice,omitempty"`
	Model      string    `json:"model,omitempty"`
	SampleRate int       `json:"sampleRate"`
	Duration   float64   `json:"duration"`
	Words      []Word    `json:"words"`
	Segments   []Segment `json:"segments"`
}

// Synth runs the scripts and calls ElevenLabs.
type Synth struct {
	UV        string // uv binary; default "uv"
	ScriptDir string // directory holding okur_say.py, ema_say.py and kokoro_say.py
	ModelDir  string // optional weights cache root
	Threads   int    // ONNX Runtime threads; 0 = all cores
	// ElevenLabsKey returns the ElevenLabs API key, read per call so a key
	// saved in the settings applies at once; nil or "" disables the engine.
	ElevenLabsKey func(context.Context) string
	ElevenLabsURL string // default elevenlabs.DefaultURL
}

// Engines.
const (
	EngineOkur       = "okur"
	EngineEMA        = "ema"
	EngineKokoro     = "kokoro"
	EngineElevenLabs = "elevenlabs"
)

// ElevenLabsOptions are the ElevenLabs model and voice settings.
type ElevenLabsOptions struct {
	Model           string  `json:"model"`
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarityBoost"`
	Style           float64 `json:"style"`
	SpeakerBoost    bool    `json:"speakerBoost"`
}

// Options shape the narration.
type Options struct {
	Engine         string             `json:"engine,omitempty"` // "" = the language's default (okur, kokoro)
	Voice          string             `json:"voice,omitempty"`
	Speed          float64            `json:"speed,omitempty"`
	Seed           int                `json:"seed,omitempty"`           // okur, ema, elevenlabs
	SentencePause  float64            `json:"sentencePause,omitempty"`  // seconds; 0 = script default
	ParagraphPause float64            `json:"paragraphPause,omitempty"` // seconds; 0 = script default
	Quality        string             `json:"quality,omitempty"`        // kokoro: fp32 | fp16 | int8
	Accent         string             `json:"accent,omitempty"`         // kokoro: en-us | en-gb
	Lexicon        map[string]string  `json:"lexicon,omitempty"`        // written word → spoken form
	ElevenLabs     *ElevenLabsOptions `json:"elevenlabs,omitempty"`
}

// scripts maps a language's local engines to their scripts.
var scripts = map[string]map[string]string{
	"en": {EngineKokoro: "kokoro_say.py"},
	"tr": {EngineOkur: "okur_say.py", EngineEMA: "ema_say.py"},
}

// defaultEngine is each language's engine when none is chosen.
var defaultEngine = map[string]string{"en": EngineKokoro, "tr": EngineOkur}

// Say synthesises the text in textFile into outDir/speech.wav and
// outDir/timings.json. lang is "en" or "tr".
func (s *Synth) Say(ctx context.Context, lang, textFile, outDir string, o Options) (Timings, error) {
	engine := o.Engine
	if engine == "" {
		engine = defaultEngine[lang]
	}
	if engine == EngineElevenLabs && defaultEngine[lang] != "" {
		return s.sayElevenLabs(ctx, lang, textFile, outDir, o)
	}
	voice, speed := o.Voice, o.Speed
	script := scripts[lang][engine]
	if script == "" {
		return Timings{}, fmt.Errorf("speech: no %s voice for language %q", engine, lang)
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
	if voice != "" && lang == "en" {
		args = append(args, "--voice", voice)
	}
	if o.SentencePause > 0 {
		args = append(args, "--sentence-pause", strconv.FormatFloat(o.SentencePause, 'f', 3, 64))
	}
	if o.ParagraphPause > 0 {
		args = append(args, "--paragraph-pause", strconv.FormatFloat(o.ParagraphPause, 'f', 3, 64))
	}
	if engine != EngineKokoro && o.Seed != 0 {
		args = append(args, "--seed", strconv.Itoa(o.Seed))
	}
	if lang == "en" {
		if o.Quality != "" {
			args = append(args, "--quality", o.Quality)
		}
		accent := o.Accent
		if accent == "" && strings.HasPrefix(voice, "b") {
			accent = "en-gb" // British voices read best with British phonemes
		}
		if accent != "" {
			args = append(args, "--accent", accent)
		}
	}
	if len(o.Lexicon) > 0 {
		raw, _ := json.Marshal(o.Lexicon)
		lex := filepath.Join(outDir, ".lexicon.json")
		if err := os.MkdirAll(outDir, 0o750); err != nil {
			return Timings{}, err
		}
		if err := os.WriteFile(lex, raw, 0o600); err != nil {
			return Timings{}, err
		}
		defer func() { _ = os.Remove(lex) }()
		args = append(args, "--lexicon", lex)
	}
	if s.ModelDir != "" {
		args = append(args, "--model-dir", filepath.Join(s.ModelDir, strings.TrimSuffix(script, "_say.py")))
	}
	cmd := exec.CommandContext(ctx, uv, args...) //nolint:gosec // fixed scripts, arguments built here
	cmd.Env = os.Environ()
	if s.Threads > 0 {
		cmd.Env = append(cmd.Env, "EKIN_SPEECH_THREADS="+strconv.Itoa(s.Threads))
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
	if lang == "en" {
		t.Voice = voice
	}
	if err := recordVoice(filepath.Join(outDir, "timings.json"), raw, t.Voice, ""); err != nil {
		return Timings{}, err
	}
	return t, nil
}

// recordVoice adds the voice and model to a script's timings.json,
// keeping every field the script wrote.
func recordVoice(path string, raw []byte, voice, model string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("speech: parse timings: %w", err)
	}
	for k, v := range map[string]string{"voice": voice, "model": model} {
		if v == "" {
			delete(fields, k)
			continue
		}
		fields[k], _ = json.Marshal(v)
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("speech: write timings: %w", err)
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
