// Package render turns a composition (an HTML page whose frames are a
// pure function of time, see kit/kurgu.js) into video: headless Chrome
// draws each frame on demand, ffmpeg encodes the screenshots and mixes
// narration with music. The page is served from 127.0.0.1 and Chrome
// resolves no other host, so a render never touches the network.
package render

import (
	"context"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

//go:embed all:kit
var kitFS embed.FS

// Kit is the composition runtime served at /kit/ (kurgu.js, kurgu.css,
// tone.js, fonts).
func Kit() fs.FS {
	sub, _ := fs.Sub(kitFS, "kit")
	return sub
}

// Renderer holds the tools and the throttling knobs (PLAN.md §4).
type Renderer struct {
	Chrome  string  // Chrome / Chromium binary; empty = autodetect
	FFmpeg  string  // default "ffmpeg"
	Encoder string  // libx264 | h264_videotoolbox
	Preset  string  // x264 preset, default "veryfast"
	CRF     int     // x264 CRF, default 20
	Threads int     // ffmpeg threads; 0 = auto
	Chunks  int     // parallel tabs; 1 on a small VPS
	Duty    float64 // 0 < duty ≤ 1; 0.25 sleeps 3× the frame time after each frame
	GPU     bool
	Quality int // JPEG quality of captured frames, default 90
}

// Meta is what a composition declares on window.kurgu.
type Meta struct {
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	FPS      float64 `json:"fps"`
	Duration float64 `json:"duration"`
}

// Frames is the number of frames at the declared rate.
func (m Meta) Frames() int { return int(math.Round(m.Duration * m.FPS)) }

// Problems collects what went wrong in the page while it loaded and
// rendered: uncaught exceptions, console errors, blocked requests.
type Problems struct {
	mu   sync.Mutex
	list []string
}

func (p *Problems) add(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.list) < 50 {
		p.list = append(p.list, s)
	}
}

// List returns the problems seen so far.
func (p *Problems) List() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.list...)
}

// session is a browser plus the local server for one working directory.
type session struct {
	r        *Renderer
	base     string
	browser  context.Context
	cancel   func()
	problems *Problems
}

func (r *Renderer) open(ctx context.Context, dir string) (*session, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/kit/", http.StripPrefix("/kit/", http.FileServerFS(Kit())))
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.Handle("/", http.FileServer(http.Dir(dir)))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	chrome := r.Chrome
	if chrome == "" {
		chrome = FindChrome()
	}
	if chrome == "" {
		_ = srv.Close()
		return nil, errors.New("render: Chrome or Chromium is not installed")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.Flag("font-render-hinting", "none"),
		chromedp.Flag("force-color-profile", "srgb"),
		chromedp.Flag("disable-lcd-text", true),
		chromedp.Flag("run-all-compositor-stages-before-draw", true),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"),
		chromedp.Flag("mute-audio", true),
	)
	if os.Geteuid() == 0 {
		opts = append(opts, chromedp.NoSandbox)
	}
	if !r.GPU {
		opts = append(opts, chromedp.Flag("disable-gpu", true))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	if err := chromedp.Do(browserCtx); err != nil {
		cancelBrowser()
		cancelAlloc()
		_ = srv.Close()
		return nil, fmt.Errorf("render: start chrome: %w", err)
	}
	return &session{
		r: r, base: "http://" + ln.Addr().String(), browser: browserCtx, problems: &Problems{},
		cancel: func() { cancelBrowser(); cancelAlloc(); _ = srv.Close() },
	}, nil
}

func (s *session) close() { s.cancel() }

// tab opens pagePath in a fresh browsing context at scale and waits for
// window.kurgu.ready.
func (s *session) tab(pagePath string, scale float64) (context.Context, Meta, error) {
	ctx, _ := chromedp.NewContext(s.browser, chromedp.WithNewBrowserContext())
	if err := chromedp.Do(ctx); err != nil {
		return nil, Meta{}, fmt.Errorf("render: open tab: %w", err)
	}
	s.watch(ctx)
	if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
		return nil, Meta{}, err
	}
	if _, err := chromedp.Call(ctx, cdpruntime.Enable, cdp.Empty{}); err != nil {
		return nil, Meta{}, err
	}
	if err := setViewport(ctx, 1080, 1920, scale); err != nil {
		return nil, Meta{}, err
	}
	if err := chromedp.Do(ctx, chromedp.Navigate(s.base+"/"+strings.TrimPrefix(pagePath, "/"))); err != nil {
		return nil, Meta{}, fmt.Errorf("render: load %s: %w", pagePath, err)
	}
	m, err := chromedp.Run(ctx, chromedp.Evaluate[Meta](`(async () => {
		if (!window.kurgu) throw new Error("the page did not call Kurgu.composition (window.kurgu is missing)");
		await window.kurgu.ready;
		const k = window.kurgu;
		return {width: k.width, height: k.height, fps: k.fps, duration: k.duration};
	})()`, chromedp.EvalAwaitPromise))
	if err != nil {
		return nil, Meta{}, fmt.Errorf("render: load %s: %w", pagePath, err)
	}
	if m.Width <= 0 || m.Height <= 0 || m.Width > 4096 || m.Height > 4096 || m.FPS <= 0 || m.FPS > 60 || m.Duration <= 0 {
		return nil, m, fmt.Errorf("render: window.kurgu declares an invalid size, fps or duration: %+v", m)
	}
	if err := setViewport(ctx, int64(m.Width), int64(m.Height), scale); err != nil {
		return nil, m, err
	}
	_, err = chromedp.Call(ctx, emulation.SetDefaultBackgroundColorOverride,
		emulation.SetDefaultBackgroundColorOverrideParams{Color: &cdp.RGBA{R: 0, G: 0, B: 0, A: 1}})
	return ctx, m, err
}

func setViewport(ctx context.Context, w, h int64, scale float64) error {
	_, err := chromedp.Call(ctx, emulation.SetDeviceMetricsOverride,
		emulation.SetDeviceMetricsOverrideParams{Width: w, Height: h, DeviceScaleFactor: scale})
	return err
}

// watch records the tab's exceptions, console errors and failed requests.
func (s *session) watch(ctx context.Context) {
	exceptions := chromedp.Events(ctx, cdpruntime.ExceptionThrown)
	console := chromedp.Events(ctx, cdpruntime.ConsoleAPICalled)
	failed := chromedp.Events(ctx, network.LoadingFailed)
	responses := chromedp.Events(ctx, network.ResponseReceived)
	go func() {
		for e, err := range exceptions {
			if err != nil {
				return
			}
			msg := e.ExceptionDetails.Text
			if ex := e.ExceptionDetails.Exception; ex != nil && ex.Description != "" {
				msg = ex.Description
			}
			s.problems.add("exception: " + firstLine(msg))
		}
	}()
	go func() {
		for e, err := range console {
			if err != nil {
				return
			}
			if e.Type != "error" {
				continue
			}
			var parts []string
			for _, a := range e.Args {
				if a.Description != "" {
					parts = append(parts, a.Description)
				} else {
					parts = append(parts, strings.Trim(fmt.Sprint(a.Value), `"`))
				}
			}
			s.problems.add("console.error: " + firstLine(strings.Join(parts, " ")))
		}
	}()
	go func() {
		for e, err := range failed {
			if err != nil {
				return
			}
			if !e.Canceled {
				s.problems.add("request failed: " + e.ErrorText)
			}
		}
	}()
	go func() {
		for e, err := range responses {
			if err != nil {
				return
			}
			if e.Response != nil && e.Response.Status >= 400 {
				s.problems.add(fmt.Sprintf("request %s answered %d", strings.TrimPrefix(e.Response.URL, s.base), e.Response.Status))
			}
		}
	}()
}

func renderAt(ctx context.Context, t float64) error {
	_, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`Promise.resolve(window.kurgu.renderAt(`+strconv.FormatFloat(t, 'f', 4, 64)+`)).then(() => true)`,
		chromedp.EvalAwaitPromise))
	return err
}

func capture(ctx context.Context, format page.CaptureScreenshotFormat, quality int) ([]byte, error) {
	fast := true
	p := page.CaptureScreenshotParams{Format: format, OptimizeForSpeed: &fast}
	if format == page.CaptureScreenshotFormatJpeg {
		q := int64(quality)
		p.Quality = &q
	}
	res, err := chromedp.Call(ctx, page.CaptureScreenshot, p)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

// Probe loads the composition, renders its first, middle and last frame,
// and returns its declaration with every problem the page reported.
func (r *Renderer) Probe(ctx context.Context, dir, pagePath string) (Meta, []string, error) {
	s, err := r.open(ctx, dir)
	if err != nil {
		return Meta{}, nil, err
	}
	defer s.close()
	tab, m, err := s.tab(pagePath, 0.25)
	if err != nil {
		return m, s.problems.List(), err
	}
	for _, t := range []float64{0, m.Duration / 2, math.Max(0, m.Duration-1/m.FPS)} {
		if err := renderAt(tab, t); err != nil {
			s.problems.add(fmt.Sprintf("renderAt(%.2f): %v", t, err))
		}
	}
	return m, s.problems.List(), nil
}

// Snapshot writes JPEG frames at the given times into outDir and returns
// their paths. It is the agent's preview_frame.
func (r *Renderer) Snapshot(ctx context.Context, dir, pagePath string, times []float64, scale float64, outDir string) ([]string, []string, error) {
	s, err := r.open(ctx, dir)
	if err != nil {
		return nil, nil, err
	}
	defer s.close()
	tab, m, err := s.tab(pagePath, scale)
	if err != nil {
		return nil, s.problems.List(), err
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return nil, nil, err
	}
	var paths []string
	for _, t := range times {
		t = math.Min(math.Max(0, t), m.Duration)
		if err := renderAt(tab, t); err != nil {
			return paths, s.problems.List(), fmt.Errorf("render: renderAt(%.2f): %w", t, err)
		}
		data, err := capture(tab, page.CaptureScreenshotFormatJpeg, 85)
		if err != nil {
			return paths, s.problems.List(), err
		}
		p := filepath.Join(outDir, "frame-"+strconv.FormatFloat(t, 'f', 2, 64)+".jpg")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			return paths, nil, err
		}
		paths = append(paths, p)
	}
	return paths, s.problems.List(), nil
}

// Video renders the composition at scale into a silent MP4 at out.
func (r *Renderer) Video(ctx context.Context, dir, pagePath string, scale float64, out string) (Meta, error) {
	s, err := r.open(ctx, dir)
	if err != nil {
		return Meta{}, err
	}
	defer s.close()
	first, m, err := s.tab(pagePath, scale)
	if err != nil {
		return m, err
	}
	frames := m.Frames()
	n := max(1, r.Chunks)
	if frames < n*30 {
		n = 1
	}
	parts := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		from, to := frames*i/n, frames*(i+1)/n
		parts[i] = out
		if n > 1 {
			parts[i] = fmt.Sprintf("%s.part%02d.mp4", strings.TrimSuffix(out, ".mp4"), i)
		}
		wg.Go(func() {
			tab := first
			if i > 0 {
				var err error
				if tab, _, err = s.tab(pagePath, scale); err != nil {
					errs[i] = err
					return
				}
			}
			errs[i] = r.encodeRange(ctx, tab, m, from, to, parts[i])
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return m, err
	}
	if probs := s.problems.List(); len(probs) > 0 {
		return m, fmt.Errorf("render: the page reported problems: %s", strings.Join(probs, "; "))
	}
	if n > 1 {
		if err := r.concat(ctx, parts, out); err != nil {
			return m, err
		}
	}
	return m, nil
}

func (r *Renderer) encodeRange(ctx context.Context, tab context.Context, m Meta, from, to int, out string) error {
	quality := r.Quality
	if quality == 0 {
		quality = 90
	}
	args := []string{"-y", "-loglevel", "error", "-f", "image2pipe", "-framerate", strconv.FormatFloat(m.FPS, 'f', -1, 64),
		"-c:v", "mjpeg", "-i", "-"}
	args = append(args, r.videoCodec()...)
	args = append(args, "-pix_fmt", "yuv420p", "-movflags", "+faststart", out)
	ff := exec.CommandContext(ctx, r.ffmpeg(), args...) //nolint:gosec // fixed binary
	var stderr strings.Builder
	ff.Stderr = &stderr
	stdin, err := ff.StdinPipe()
	if err != nil {
		return err
	}
	if err := ff.Start(); err != nil {
		return fmt.Errorf("render: start ffmpeg: %w", err)
	}
	var loopErr error
	for f := from; f < to && loopErr == nil; f++ {
		start := time.Now()
		if loopErr = renderAt(tab, float64(f)/m.FPS); loopErr != nil {
			loopErr = fmt.Errorf("render: frame %d: %w", f, loopErr)
			break
		}
		data, err := capture(tab, page.CaptureScreenshotFormatJpeg, quality)
		if err != nil {
			loopErr = fmt.Errorf("render: capture frame %d: %w", f, err)
			break
		}
		if _, err := stdin.Write(data); err != nil {
			loopErr = fmt.Errorf("render: ffmpeg stopped: %w: %s", err, stderr.String())
			break
		}
		if r.Duty > 0 && r.Duty < 1 {
			time.Sleep(time.Duration(float64(time.Since(start)) * (1/r.Duty - 1)))
		}
	}
	_ = stdin.Close()
	waitErr := ff.Wait()
	if loopErr != nil {
		return loopErr
	}
	if waitErr != nil {
		return fmt.Errorf("render: ffmpeg: %w: %s", waitErr, stderr.String())
	}
	return nil
}

func (r *Renderer) videoCodec() []string {
	var args []string
	if r.Encoder == "h264_videotoolbox" {
		args = []string{"-c:v", "h264_videotoolbox", "-q:v", "65", "-allow_sw", "1"}
	} else {
		preset, crf := r.Preset, r.CRF
		if preset == "" {
			preset = "veryfast"
		}
		if crf == 0 {
			crf = 20
		}
		args = []string{"-c:v", "libx264", "-preset", preset, "-crf", strconv.Itoa(crf)}
	}
	if r.Threads > 0 {
		args = append(args, "-threads", strconv.Itoa(r.Threads), "-filter_threads", "1")
	}
	return args
}

func (r *Renderer) ffmpeg() string {
	if r.FFmpeg != "" {
		return r.FFmpeg
	}
	return "ffmpeg"
}

func (r *Renderer) concat(ctx context.Context, parts []string, out string) error {
	list := out + ".txt"
	var b strings.Builder
	for _, p := range parts {
		abs, _ := filepath.Abs(p)
		fmt.Fprintf(&b, "file '%s'\n", strings.ReplaceAll(abs, "'", `'\''`))
	}
	if err := os.WriteFile(list, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if err := r.run(ctx, "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", list, "-c", "copy", "-movflags", "+faststart", out); err != nil {
		return err
	}
	for _, p := range append(parts, list) {
		_ = os.Remove(p)
	}
	return nil
}

func (r *Renderer) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, r.ffmpeg(), args...) //nolint:gosec // fixed binary
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("render: ffmpeg is not installed")
		}
		return fmt.Errorf("render: ffmpeg: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Music renders dir/music.js offline (Tone.js) to a WAV of duration
// seconds and returns its peak level.
func (r *Renderer) Music(ctx context.Context, dir string, duration float64, out string) (float64, error) {
	s, err := r.open(ctx, dir)
	if err != nil {
		return 0, err
	}
	defer s.close()
	tab, _ := chromedp.NewContext(s.browser)
	type wav struct {
		Base64 string  `json:"base64"`
		Peak   float64 `json:"peak"`
	}
	if err := chromedp.Do(tab, chromedp.Navigate(s.base+"/kit/music.html")); err != nil {
		return 0, fmt.Errorf("render: music page: %w", err)
	}
	res, err := chromedp.Run(tab, chromedp.Evaluate[wav](
		`window.renderMusic(`+strconv.FormatFloat(duration, 'f', 3, 64)+`, 44100)`, chromedp.EvalAwaitPromise))
	if err != nil {
		return 0, fmt.Errorf("render: music.js: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(res.Base64)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(out, raw, 0o600); err != nil {
		return 0, err
	}
	return res.Peak, nil
}

// Mix muxes narration (and music, ducked under the voice) onto a silent
// video, normalised to -14 LUFS for YouTube. music may be empty.
func (r *Renderer) Mix(ctx context.Context, video, narration, music string, duration float64, out string) error {
	args := []string{"-y", "-loglevel", "error", "-i", video, "-i", narration}
	var filter string
	if music != "" {
		args = append(args, "-i", music)
		filter = "[1:a]aresample=48000,apad,asplit=2[n1][n2];" +
			"[2:a]aresample=48000,volume=0.28,apad[m];" +
			"[m][n1]sidechaincompress=threshold=0.04:ratio=6:attack=15:release=350[duck];" +
			"[n2][duck]amix=inputs=2:duration=first:normalize=0,loudnorm=I=-14:TP=-1.5:LRA=11[a]"
	} else {
		filter = "[1:a]aresample=48000,apad,loudnorm=I=-14:TP=-1.5:LRA=11[a]"
	}
	args = append(args, "-filter_complex", filter, "-map", "0:v", "-map", "[a]",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "160k", "-ar", "48000",
		"-t", strconv.FormatFloat(duration, 'f', 3, 64), "-movflags", "+faststart", out)
	return r.run(ctx, args...)
}

// Poster extracts a JPEG frame at t seconds.
func (r *Renderer) Poster(ctx context.Context, video string, t float64, out string) error {
	return r.run(ctx, "-y", "-loglevel", "error", "-ss", strconv.FormatFloat(t, 'f', 2, 64), "-i", video,
		"-frames:v", "1", "-q:v", "3", out)
}

// Transcode converts a stock clip to a short, silent, seekable WebM the
// composition can scrub frame by frame (Chromium builds without H.264
// cannot decode the MP4s stock sites serve). maxSide bounds the long side.
func (r *Renderer) Transcode(ctx context.Context, in, out string, maxSeconds float64, maxSide int) error {
	scale := fmt.Sprintf("scale='if(gt(iw,ih),min(%d,iw),-2)':'if(gt(iw,ih),-2,min(%d,ih))'", maxSide, maxSide)
	return r.run(ctx, "-y", "-loglevel", "error", "-i", in, "-t", strconv.FormatFloat(maxSeconds, 'f', 1, 64),
		"-an", "-vf", scale+",fps=30", "-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8",
		"-b:v", "3M", "-g", "15", out)
}

// FindChrome returns the first Chrome or Chromium found, or "".
func FindChrome() string {
	candidates := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "headless_shell"}
	if runtime.GOOS == "darwin" {
		mac := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
		for _, p := range mac {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
