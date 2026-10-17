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
	"slices"
	"strconv"
	"strings"
	"time"

	"kurgu/internal/adapter/pexels"
	"kurgu/internal/adapter/render"
	"kurgu/internal/adapter/speech"
	"kurgu/internal/blob"
	librarydomain "kurgu/internal/domain/library/v1"
)

// blobFetcher fetches library files straight from the blob store, on the
// host that holds it.
type blobFetcher struct{ store blob.Store }

func (f blobFetcher) Fetch(ctx context.Context, key, dest string) error {
	rc, _, err := f.store.Open(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	out, err := os.Create(dest) //nolint:gosec // the agent's working directory
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// Spec is what the deliverable must satisfy, passed as JSON in
// KURGU_TOOL_SPEC by the worker.
type Spec struct {
	Language    string  `json:"language"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	MinDuration float64 `json:"minDuration"`
	MaxDuration float64 `json:"maxDuration"`
	// Speech is the narration voice and its options.
	Speech speech.Options `json:"speech"`
	// Long marks long-form video: metadata needs YouTube chapters.
	Long bool `json:"long,omitempty"`
	// Media says, per kind, whether music and stock come from the library,
	// are made fresh, or both.
	Media librarydomain.Policy `json:"media,omitzero"`
	// Clip marks a short cut from a long video: it speaks the long
	// video's narration, already in narration/, so tts is refused.
	Clip bool `json:"clip,omitempty"`
	// Thumbnails is how many thumbnail variants a long video needs; 0
	// keeps a frame of the video.
	Thumbnails int `json:"thumbnails,omitempty"`
}

// Env is the tool configuration read from the environment.
type Env struct {
	Spec     Spec
	Speech   speech.Synth
	Pexels   pexels.Client
	Renderer render.Renderer
	// Library fetches library files; nil when the library is not
	// reachable from this host.
	Library   Fetcher
	Dir       string // working directory
	Stdout    io.Writer
	Stderr    io.Writer
	Transcode bool
}

// FromEnviron builds Env from KURGU_* variables.
func FromEnviron() (Env, error) {
	e := Env{
		Speech: speech.Synth{UV: os.Getenv("KURGU_UV"), ScriptDir: os.Getenv("KURGU_SPEECH_DIR"), ModelDir: os.Getenv("KURGU_SPEECH_MODELS"),
			ElevenLabsKey: func(context.Context) string { return os.Getenv("KURGU_ELEVENLABS_API_KEY") }},
		Pexels:    pexels.Client{APIKey: os.Getenv("KURGU_PEXELS_API_KEY")},
		Renderer:  render.Renderer{Chrome: os.Getenv("KURGU_CHROME"), FFmpeg: os.Getenv("KURGU_FFMPEG")},
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Transcode: true,
	}
	if store, err := blob.FromEnv(os.Getenv); err == nil {
		e.Library = blobFetcher{store}
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
  library [--type music|video|photo] [--query "calm piano"]
                                                   search this video's media library
  library use <id>                                 put a library item in place and credit it
  clip   --first N --last M                        a short cut from a long video: speak sentences N–M
                                                   of the window in sources/clip.md instead
  thumbnail [--n 2]                                draw and check thumbnail.html's variants into
                                                   thumbnails/<n>.jpg and <n>.small.jpg
  thumbnail --stills 12.4,30                       full-size video frames for the thumbnails, into
                                                   thumbnails/stills/<t>.jpg
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
	case "clip":
		first := fs.Int("first", 0, "first sentence of the window to speak (1-based)")
		last := fs.Int("last", 0, "last sentence of the window to speak")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return e.clip(*first, *last)
	case "thumbnail":
		stills := fs.String("stills", "", "comma-separated composition times to render as stills")
		n := fs.Int("n", 0, "draw only this variant")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return e.thumbnail(ctx, *stills, *n)
	case "library":
		kind := fs.String("type", "", "music, video or photo; default all")
		query := fs.String("query", "", "words to look for in titles, tags and descriptions")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return e.library(ctx, fs.Args(), *kind, *query)
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
	if e.Spec.Clip {
		return errors.New("this short speaks the long video's narration: narration/speech.wav and narration/timings.json " +
			"are already in place and must stay as they are; do not run tts")
	}
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
	t, err := e.Speech.Say(ctx, lang, e.path(textFile), out, e.Spec.Speech)
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
// references nothing on the network. When r is also a Sampler, it checks
// the picture too: no caption-only stretches, no long stills.
func Check(ctx context.Context, r Prober, dir, pagePath string, spec Spec) Report {
	rep := Report{}
	fail := func(format string, a ...any) { rep.Errors = append(rep.Errors, fmt.Sprintf(format, a...)) }
	spec, err := clipSpec(dir, spec)
	if err != nil {
		fail("%v", err)
	}
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
	} else if err := checkMetadata(filepath.Join(dir, "metadata.json"), spec.Long, narration); err != nil {
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
		if spec.Clip && math.Abs(narration-spec.MinDuration) > 0.1 {
			fail("narration/timings.json lasts %.2f s, not the clip's %.2f s; the long video's narration must stay as it was prepared", narration, spec.MinDuration)
		}
		if narration > 0 && (m.Duration < narration || m.Duration > narration+4) {
			fail("duration %.2f s must cover the narration (%.2f s) plus at most 4 s of tail", m.Duration, narration)
		}
		if m.FPS != 30 && m.FPS != 25 && m.FPS != 60 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("fps %.0f is unusual; use 30", m.FPS))
		}
		if smp, ok := r.(Sampler); ok && len(rep.Errors) == 0 {
			samples, problems, err := smp.Sample(ctx, dir, pagePath, sampleEvery)
			if err != nil {
				fail("%v", err)
			}
			for _, p := range problems {
				fail("page: %s", p)
			}
			errs, warns := checkPicture(samples, narration, spec.Long)
			rep.Errors = append(rep.Errors, errs...)
			rep.Warnings = append(rep.Warnings, warns...)
		}
	}
	rep.Errors = append(rep.Errors, CheckMedia(dir, spec)...)
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

func checkMetadata(p string, long bool, duration float64) error {
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
	if long {
		// YouTube shows chapters only with at least three, ascending, each
		// at least 10 seconds long.
		if len(m.Chapters) < 3 {
			return errors.New("a long video needs at least 3 chapters")
		}
		for i := 1; i < len(m.Chapters); i++ {
			if m.Chapters[i].At-m.Chapters[i-1].At < 10 {
				return fmt.Errorf("chapter %q starts less than 10 s after the previous one", m.Chapters[i].Title)
			}
			if duration > 0 && m.Chapters[i].At >= duration {
				return fmt.Errorf("chapter %q starts after the narration ends", m.Chapters[i].Title)
			}
		}
	}
	return nil
}

// Credit is one stock or library item's attribution (assets/credits.json).
type Credit struct {
	File    string `json:"file"`
	Source  string `json:"source"`
	Kind    string `json:"kind,omitempty"` // video, photo or music
	Author  string `json:"author"`
	URL     string `json:"url"`
	Licence string `json:"licence,omitempty"`
	Query   string `json:"query,omitempty"`
	Seconds int    `json:"seconds,omitempty"`
	// Library is the library asset the file came from; "" for stock
	// fetched from Pexels.
	Library string `json:"library,omitempty"`
	// For Pexels stock: its id, size and alt text, and the file the stored
	// one was cut from.
	ExternalID  string `json:"externalId,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Alt         string `json:"alt,omitempty"`
	OriginalURL string `json:"originalUrl,omitempty"`
}

// MusicMeta is music.meta.json: how the agent describes a score it
// composed, for the library.
type MusicMeta struct {
	Title       string `json:"title"`
	Mood        string `json:"mood,omitempty"`
	Tempo       int    `json:"tempo,omitempty"`
	Description string `json:"description,omitempty"`
}

// ReadMusicMeta reads music.meta.json; the zero value when absent or
// unreadable.
func ReadMusicMeta(dir string) MusicMeta {
	var m MusicMeta
	if raw, err := os.ReadFile(filepath.Join(dir, "music.meta.json")); err == nil { //nolint:gosec // agent dir
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// ReadCredits reads assets/credits.json; none when absent.
func ReadCredits(dir string) []Credit {
	return readCredits(filepath.Join(dir, "assets", "credits.json"))
}

func (e Env) stock(ctx context.Context, query, kind string, count int) error {
	if strings.TrimSpace(query) == "" {
		return errors.New("--query is required")
	}
	if kind != "video" && kind != "photo" {
		return errors.New("--type must be video or photo")
	}
	if !e.Spec.Media.AllowsFresh(librarydomain.Kind(kind)) {
		return fmt.Errorf("this video takes %ss from the library only: run kurgu tool library --type %s", kind, kind)
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
		credits = slices.DeleteFunc(credits, func(c Credit) bool { return c.File == rel })
		credits = append(credits, Credit{File: rel, Source: "Pexels", Kind: it.Kind, Author: it.Author, URL: it.PageURL, Query: query,
			ExternalID: strconv.Itoa(it.ID), Width: it.Width, Height: it.Height, Alt: it.Alt, OriginalURL: it.OriginalURL,
			Seconds: int(math.Min(it.Duration, 15))})
		desc := fmt.Sprintf("%s  (%dx%d", rel, it.Width, it.Height)
		if it.Kind == "video" {
			desc += fmt.Sprintf(", %.0f s usable, loop it with Kurgu.video(el).seek(t)", math.Min(it.Duration, 15))
		}
		_, _ = fmt.Fprintln(e.Stdout, desc+")")
	}
	return writeCredits(assets, credits)
}

func writeCredits(assets string, credits []Credit) error {
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
		if _, ok, _ := ReadMusicTrack(e.Dir); ok {
			return errors.New("music.json uses a library track; there is nothing to render")
		}
		return errors.New("music.js is missing")
	}
	if !e.Spec.Media.AllowsFresh(librarydomain.KindMusic) {
		if problems := CheckMedia(e.Dir, e.Spec); len(problems) > 0 {
			return errors.New(strings.Join(problems, "; "))
		}
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
