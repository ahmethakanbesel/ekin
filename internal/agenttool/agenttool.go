// Package agenttool implements `kurgu tool`, the commands the authoring
// agent runs from its working directory: synthesise narration, look at a
// frame, check the composition, fetch stock media, render the music.
// They need no database; the worker passes their settings through the
// environment of the agent process.
package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kurgu/internal/adapter/pexels"
	"kurgu/internal/adapter/render"
	"kurgu/internal/adapter/speech"
)

// Spec is what the deliverable must satisfy, passed as JSON in
// KURGU_TOOL_SPEC by the worker.
type Spec struct {
	Language    string  `json:"language"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	MinDuration float64 `json:"minDuration"`
	MaxDuration float64 `json:"maxDuration"`
	Voice       string  `json:"voice,omitempty"`
	Speed       float64 `json:"speed,omitempty"`
}

// Env is the tool configuration read from the environment.
type Env struct {
	Spec      Spec
	Speech    speech.Synth
	Pexels    pexels.Client
	Renderer  render.Renderer
	Dir       string // working directory
	Stdout    io.Writer
	Stderr    io.Writer
	Transcode bool
}

// FromEnviron builds Env from KURGU_* variables.
func FromEnviron() (Env, error) {
	e := Env{
		Speech:    speech.Synth{UV: os.Getenv("KURGU_UV"), ScriptDir: os.Getenv("KURGU_SPEECH_DIR"), ModelDir: os.Getenv("KURGU_SPEECH_MODELS")},
		Pexels:    pexels.Client{APIKey: os.Getenv("KURGU_PEXELS_API_KEY")},
		Renderer:  render.Renderer{Chrome: os.Getenv("KURGU_CHROME"), FFmpeg: os.Getenv("KURGU_FFMPEG")},
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Transcode: true,
	}
	if raw := os.Getenv("KURGU_TOOL_SPEC"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &e.Spec); err != nil {
			return e, fmt.Errorf("KURGU_TOOL_SPEC: %w", err)
		}
	}
	dir, err := os.Getwd()
	e.Dir = dir
	return e, err
}

const usage = `usage: kurgu tool <command> [flags]

  tts    [--text-file narration/narration.txt]     narration → narration/speech.wav + timings.json
  frame  --at 0,2.5,10 [--page composition.html]   render frames to frames/*.jpg (read them to check)
  check  [--page composition.html]                 validate the composition against the spec
  stock  --query "calm sea" [--type video|photo] [--count 1]
                                                   download stock media into assets/
  music  [--duration seconds]                      render music.js to music.wav
`

// Run executes one tool command.
func Run(ctx context.Context, e Env, args []string) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(e.Stderr, usage)
		return errors.New("missing tool command")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	switch args[0] {
	case "tts":
		textFile := fs.String("text-file", "narration/narration.txt", "narration text")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return e.tts(ctx, *textFile)
	case "frame":
		at := fs.String("at", "0", "comma-separated times in seconds")
		pagePath := fs.String("page", "composition.html", "composition page")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return e.frame(ctx, *pagePath, *at)
	case "check":
		pagePath := fs.String("page", "composition.html", "composition page")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		report := Check(ctx, &e.Renderer, e.Dir, *pagePath, e.Spec)
		b, _ := json.MarshalIndent(report, "", "  ")
		_, _ = fmt.Fprintln(e.Stdout, string(b))
		if !report.OK {
			return errors.New("check failed: fix every error above and run kurgu tool check again")
		}
		return nil
	case "stock":
		query := fs.String("query", "", "what to search for, in English")
		kind := fs.String("type", "video", "video or photo")
		count := fs.Int("count", 1, "how many (1-5)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return e.stock(ctx, *query, *kind, *count)
	case "music":
		duration := fs.Float64("duration", 0, "seconds; default the narration length")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return e.music(ctx, *duration)
	}
	_, _ = fmt.Fprint(e.Stderr, usage)
	return fmt.Errorf("unknown tool command %q", args[0])
}

func (e Env) path(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(e.Dir, p)
}

func (e Env) tts(ctx context.Context, textFile string) error {
	lang := e.Spec.Language
	if lang == "" {
		lang = "tr"
	}
	raw, err := os.ReadFile(e.path(textFile))
	if err != nil {
		return fmt.Errorf("read %s: %w (write the narration there first)", textFile, err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return fmt.Errorf("%s is empty", textFile)
	}
	out := e.path("narration")
	if err := os.MkdirAll(out, 0o750); err != nil {
		return err
	}
	t, err := e.Speech.Say(ctx, lang, e.path(textFile), out, e.Spec.Voice, e.Spec.Speed)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("narration/speech.wav: %.2f s, %d words; timings in narration/timings.json (load with Kurgu.timings())", t.Duration, len(t.Words))
	if e.Spec.MaxDuration > 0 && t.Duration > e.Spec.MaxDuration {
		msg += fmt.Sprintf("\nWARNING: longer than the %.0f s maximum; shorten the narration and run tts again", e.Spec.MaxDuration)
	}
	if e.Spec.MinDuration > 0 && t.Duration < e.Spec.MinDuration {
		msg += fmt.Sprintf("\nWARNING: shorter than the %.0f s minimum; lengthen the narration and run tts again", e.Spec.MinDuration)
	}
	_, _ = fmt.Fprintln(e.Stdout, msg)
	return nil
}

func (e Env) frame(ctx context.Context, pagePath, at string) error {
	var times []float64
	for part := range strings.SplitSeq(at, ",") {
		t, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return fmt.Errorf("--at: %q is not a number of seconds", part)
		}
		times = append(times, t)
	}
	if len(times) > 12 {
		return errors.New("--at: at most 12 frames at a time")
	}
	paths, problems, err := e.Renderer.Snapshot(ctx, e.Dir, pagePath, times, 0.5, e.path("frames"))
	for _, p := range paths {
		rel, _ := filepath.Rel(e.Dir, p)
		_, _ = fmt.Fprintln(e.Stdout, rel)
	}
	for _, p := range problems {
		_, _ = fmt.Fprintln(e.Stdout, "PROBLEM: "+p)
	}
	return err
}

// Report is the result of `kurgu tool check`.
type Report struct {
	OK       bool        `json:"ok"`
	Meta     render.Meta `json:"meta"`
	Errors   []string    `json:"errors"`
	Warnings []string    `json:"warnings,omitempty"`
}

var externalRef = regexp.MustCompile(`(?i)(src|href)\s*=\s*["'](https?:)?//|url\(\s*["']?(https?:)?//|import\s+[^;]*["']https?://|fetch\(\s*["']https?://`)

// Prober loads a composition (render.Renderer).
type Prober interface {
	Probe(ctx context.Context, dir, pagePath string) (render.Meta, []string, error)
}

// Check validates a composition: it loads and renders without errors,
// matches the spec's frame size and duration, follows the narration, and
// references nothing on the network.
func Check(ctx context.Context, r Prober, dir, pagePath string, spec Spec) Report {
	rep := Report{}
	fail := func(format string, a ...any) { rep.Errors = append(rep.Errors, fmt.Sprintf(format, a...)) }
	src, err := os.ReadFile(filepath.Join(dir, pagePath)) //nolint:gosec // the agent's own file
	if err != nil {
		fail("cannot read %s: %v", pagePath, err)
		return rep
	}
	if externalRef.Match(src) {
		fail("%s references the network; download media with kurgu tool stock and use relative paths", pagePath)
	}
	if !strings.Contains(string(src), "/kit/kurgu.js") {
		fail("%s must load /kit/kurgu.js and call Kurgu.composition", pagePath)
	}
	for _, banned := range []string{"requestAnimationFrame", "setInterval", "Date.now", "performance.now"} {
		if strings.Contains(string(src), banned) {
			fail("%s uses %s; frames must be a pure function of t passed to render(t)", pagePath, banned)
		}
	}
	var narration float64
	if raw, err := os.ReadFile(filepath.Join(dir, "narration", "timings.json")); err != nil { //nolint:gosec // agent dir
		fail("narration/timings.json is missing; write narration/narration.txt and run kurgu tool tts")
	} else {
		var t speech.Timings
		if json.Unmarshal(raw, &t) == nil {
			narration = t.Duration
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); err != nil {
		fail("metadata.json is missing")
	} else if err := checkMetadata(filepath.Join(dir, "metadata.json")); err != nil {
		fail("metadata.json: %v", err)
	}
	m, problems, err := r.Probe(ctx, dir, pagePath)
	rep.Meta = m
	if err != nil {
		fail("%v", err)
	}
	for _, p := range problems {
		fail("page: %s", p)
	}
	if err == nil {
		if spec.Width > 0 && (m.Width != spec.Width || m.Height != spec.Height) {
			fail("frame is %dx%d; the spec needs %dx%d", m.Width, m.Height, spec.Width, spec.Height)
		}
		if spec.MaxDuration > 0 && m.Duration > spec.MaxDuration+0.5 {
			fail("duration %.1f s is over the %.0f s maximum", m.Duration, spec.MaxDuration)
		}
		if spec.MinDuration > 0 && m.Duration < spec.MinDuration-0.5 {
			fail("duration %.1f s is under the %.0f s minimum", m.Duration, spec.MinDuration)
		}
		if narration > 0 && (m.Duration < narration || m.Duration > narration+4) {
			fail("duration %.2f s must cover the narration (%.2f s) plus at most 4 s of tail", m.Duration, narration)
		}
		if m.FPS != 30 && m.FPS != 25 && m.FPS != 60 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("fps %.0f is unusual; use 30", m.FPS))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "music.js")); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "music.wav")); err != nil {
			rep.Warnings = append(rep.Warnings, "music.js has not been rendered; run kurgu tool music to hear-check its level")
		}
	}
	rep.OK = len(rep.Errors) == 0
	return rep
}

// Metadata is the video's YouTube metadata (metadata.json).
type Metadata struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	Chapters    []Chapter `json:"chapters,omitempty"`
}

// Chapter is a long-form chapter marker.
type Chapter struct {
	At    float64 `json:"at"`
	Title string  `json:"title"`
}

func checkMetadata(p string) error {
	raw, err := os.ReadFile(p) //nolint:gosec // agent dir
	if err != nil {
		return err
	}
	var m Metadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	switch {
	case strings.TrimSpace(m.Title) == "":
		return errors.New("title is required")
	case len([]rune(m.Title)) > 100:
		return errors.New("title is over YouTube's 100 characters")
	case len([]rune(m.Description)) > 5000:
		return errors.New("description is over 5000 characters")
	case strings.ContainsAny(m.Title+m.Description, "<>"):
		return errors.New("YouTube rejects < and > in titles and descriptions")
	}
	total := 0
	for _, t := range m.Tags {
		total += len([]rune(t)) + 1
	}
	if total > 500 {
		return errors.New("tags are over YouTube's 500 characters")
	}
	if len(m.Chapters) > 0 && m.Chapters[0].At != 0 {
		return errors.New("the first chapter must start at 0")
	}
	return nil
}

// Credit is one stock item's attribution (assets/credits.json).
type Credit struct {
	File    string `json:"file"`
	Source  string `json:"source"`
	Author  string `json:"author"`
	URL     string `json:"url"`
	Query   string `json:"query"`
	Seconds int    `json:"seconds,omitempty"`
}

func (e Env) stock(ctx context.Context, query, kind string, count int) error {
	if strings.TrimSpace(query) == "" {
		return errors.New("--query is required")
	}
	if kind != "video" && kind != "photo" {
		return errors.New("--type must be video or photo")
	}
	count = max(1, min(count, 5))
	orientation := pexels.Orientation("portrait")
	minSide := 1080
	if e.Spec.Width > e.Spec.Height {
		orientation = "landscape"
	} else if e.Spec.Width == e.Spec.Height && e.Spec.Width > 0 {
		orientation = "square"
	}
	if e.Spec.Width > 0 {
		minSide = min(e.Spec.Width, e.Spec.Height)
	}
	items, err := e.Pexels.Search(ctx, kind, query, orientation, count, minSide)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("nothing found for %q; try a simpler English query", query)
	}
	assets := e.path("assets")
	if err := os.MkdirAll(assets, 0o750); err != nil {
		return err
	}
	credits := readCredits(filepath.Join(assets, "credits.json"))
	for _, it := range items {
		name := fmt.Sprintf("pexels-%s-%d", it.Kind, it.ID)
		var file string
		if it.Kind == "video" {
			raw := filepath.Join(assets, name+".src.mp4")
			if err := e.Pexels.Download(ctx, it, raw); err != nil {
				return err
			}
			file = filepath.Join(assets, name+".webm")
			maxSide := max(e.Spec.Width, e.Spec.Height)
			if maxSide == 0 {
				maxSide = 1920
			}
			tctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			err := e.Renderer.Transcode(tctx, raw, file, 15, maxSide)
			cancel()
			_ = os.Remove(raw)
			if err != nil {
				return err
			}
		} else {
			file = filepath.Join(assets, name+".jpg")
			if err := e.Pexels.Download(ctx, it, file); err != nil {
				return err
			}
		}
		rel, _ := filepath.Rel(e.Dir, file)
		credits = append(credits, Credit{File: rel, Source: "Pexels", Author: it.Author, URL: it.PageURL, Query: query,
			Seconds: int(math.Min(it.Duration, 15))})
		desc := fmt.Sprintf("%s  (%dx%d", rel, it.Width, it.Height)
		if it.Kind == "video" {
			desc += fmt.Sprintf(", %.0f s usable, loop it with Kurgu.video(el).seek(t)", math.Min(it.Duration, 15))
		}
		_, _ = fmt.Fprintln(e.Stdout, desc+")")
	}
	b, _ := json.MarshalIndent(credits, "", "  ")
	return os.WriteFile(filepath.Join(assets, "credits.json"), b, 0o600)
}

func readCredits(p string) []Credit {
	var out []Credit
	if raw, err := os.ReadFile(p); err == nil { //nolint:gosec // agent dir
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (e Env) music(ctx context.Context, duration float64) error {
	if _, err := os.Stat(e.path("music.js")); err != nil {
		return errors.New("music.js is missing")
	}
	if duration <= 0 {
		raw, err := os.ReadFile(e.path("narration/timings.json"))
		if err != nil {
			return errors.New("pass --duration or run kurgu tool tts first")
		}
		var t speech.Timings
		_ = json.Unmarshal(raw, &t)
		duration = t.Duration + 2
	}
	peak, err := e.Renderer.Music(ctx, e.Dir, duration, e.path("music.wav"))
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("music.wav: %.1f s, peak %.2f", duration, peak)
	if peak < 0.05 {
		msg += "\nWARNING: nearly silent; check that notes are scheduled on the transport"
	}
	_, _ = fmt.Fprintln(e.Stdout, msg)
	return nil
}
