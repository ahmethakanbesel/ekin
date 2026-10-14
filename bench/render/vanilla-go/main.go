// Command kurgu-render-bench renders a vanilla composition (window.kurgu.renderAt(t))
// to MP4 with chromedp + ffmpeg. It is the benchmark prototype of the production driver.
//
// Usage (from bench/render):
//
//	go run ./vanilla-go -page vanilla/composition.html -out out/vanilla.mp4
//	go run ./vanilla-go -at 0.5,4.3,12.6 -out out/snap      # preview_frame(t) snapshots
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
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
	"github.com/chromedp/cdproto/headlessexperimental"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

type config struct {
	root, page, out, chrome, capture, format, encoder, preset, extraFlags, at string
	scale                                                                     float64
	quality, crf, threads, chunks, maxFrames                                  int
	gpu, isolate, bgOverride, browsers                                        bool
	duty                                                                      float64
	timeout                                                                   time.Duration
}

type meta struct {
	Duration float64 `json:"duration"`
	FPS      float64 `json:"fps"`
	Width    int64   `json:"width"`
	Height   int64   `json:"height"`
}

type stats struct {
	Frames     int     `json:"frames"`
	Eval       float64 `json:"eval_s"`
	Capture    float64 `json:"capture_s"`
	Damageless int     `json:"no_damage_frames,omitempty"`
}

func defaultChrome() string {
	if p := os.Getenv("CHROME"); p != "" {
		return p
	}
	if runtime.GOOS == "darwin" {
		return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	for _, p := range []string{"/usr/bin/chromium", "/usr/bin/chromium-headless-shell", "/headless-shell/headless-shell", "/usr/bin/google-chrome"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "chromium"
}

func main() {
	var c config
	flag.StringVar(&c.root, "root", ".", "directory served over http (bench/render)")
	flag.StringVar(&c.page, "page", "vanilla/composition.html", "composition path relative to -root")
	flag.StringVar(&c.out, "out", "out/vanilla.mp4", "output mp4 (or snapshot prefix with -at)")
	flag.StringVar(&c.chrome, "chrome", defaultChrome(), "chrome / chromium / headless-shell binary")
	flag.StringVar(&c.capture, "capture", "screenshot", "screenshot | screenshot-slow | beginframe")
	flag.StringVar(&c.format, "format", "jpeg", "frame format: jpeg | png")
	flag.IntVar(&c.quality, "quality", 90, "jpeg quality")
	flag.Float64Var(&c.scale, "scale", 1, "output scale via deviceScaleFactor (0.5 = 540x960)")
	flag.StringVar(&c.encoder, "encoder", "libx264", "libx264 | h264_videotoolbox")
	flag.StringVar(&c.preset, "preset", "medium", "x264 preset")
	flag.IntVar(&c.crf, "crf", 20, "x264 crf")
	flag.IntVar(&c.threads, "threads", 0, "ffmpeg -threads (0 = auto)")
	flag.IntVar(&c.chunks, "chunks", 1, "parallel tabs, each encoding a contiguous frame range")
	flag.BoolVar(&c.isolate, "isolate", true, "one browser context (renderer process) per chunk")
	flag.BoolVar(&c.browsers, "browsers", false, "one Chrome process per chunk instead of one tab per chunk")
	flag.IntVar(&c.maxFrames, "frames", 0, "render only the first N frames (0 = all)")
	flag.BoolVar(&c.gpu, "gpu", false, "allow GPU (default passes --disable-gpu)")
	flag.BoolVar(&c.bgOverride, "bg", true, "Emulation.setDefaultBackgroundColorOverride to opaque black")
	flag.StringVar(&c.extraFlags, "chrome-flags", "", "extra chrome flags, comma separated: single-process,renderer-process-limit=1")
	flag.Float64Var(&c.duty, "duty", 1, "render.duty_cycle: 0.25 sleeps 3x the frame time after each frame")
	flag.DurationVar(&c.timeout, "timeout", 15*time.Minute, "abort the whole run after this long")
	flag.StringVar(&c.at, "at", "", "comma separated times: write PNG snapshots instead of a video")
	flag.Parse()

	if err := run(c); err != nil {
		log.Fatal(err)
	}
}

func run(c config) error {
	t0 := time.Now()
	root, err := filepath.Abs(c.root)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	go http.Serve(ln, http.FileServer(http.Dir(root))) //nolint:errcheck
	url := fmt.Sprintf("http://%s/%s", ln.Addr(), c.page)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(c.chrome),
		chromedp.Flag("font-render-hinting", "none"),
		chromedp.Flag("force-color-profile", "srgb"),
		chromedp.Flag("disable-lcd-text", true),
		chromedp.Flag("run-all-compositor-stages-before-draw", true),
		// Determinism: nothing but the local file server resolves.
		chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"),
		chromedp.WindowSize(1080, 1920),
	)
	if os.Geteuid() == 0 {
		opts = append(opts, chromedp.NoSandbox)
	}
	if !c.gpu {
		opts = append(opts, chromedp.Flag("disable-gpu", true))
	}
	if c.capture == "beginframe" {
		opts = append(opts,
			chromedp.Flag("enable-begin-frame-control", true),
			chromedp.Flag("deterministic-mode", true),
		)
	}
	for _, f := range strings.Split(c.extraFlags, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		k, v, ok := strings.Cut(f, "=")
		if ok {
			opts = append(opts, chromedp.Flag(k, v))
		} else {
			opts = append(opts, chromedp.Flag(k, true))
		}
	}
	runCtx, cancelRun := context.WithTimeout(context.Background(), c.timeout)
	defer cancelRun()
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(runCtx, opts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	if err := chromedp.Do(browserCtx); err != nil {
		return fmt.Errorf("start chrome: %w", err)
	}
	launched := time.Since(t0)

	if c.at != "" {
		return snapshots(browserCtx, c, url, t0)
	}

	// Probe the composition once for its metadata.
	probe, m, err := openTab(browserCtx, c, url, false, true)
	if err != nil {
		return err
	}
	frames := int(math.Round(m.Duration * m.FPS))
	if c.maxFrames > 0 && c.maxFrames < frames {
		frames = c.maxFrames
	}
	loaded := time.Since(t0)

	if err := os.MkdirAll(filepath.Dir(c.out), 0o755); err != nil {
		return err
	}
	n := max(1, c.chunks)
	parts := make([]string, n)
	results := make([]stats, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		from, to := frames*i/n, frames*(i+1)/n
		parts[i] = c.out
		if n > 1 {
			parts[i] = fmt.Sprintf("%s.part%02d.mp4", strings.TrimSuffix(c.out, ".mp4"), i)
		}
		tab := probe
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i > 0 && c.browsers {
				bctx, cancel := chromedp.NewContext(allocCtx2(runCtx, opts))
				defer cancel()
				var err error
				if tab, _, err = openTab(bctx, c, url, false, true); err != nil {
					errs[i] = err
					return
				}
			} else if i > 0 {
				var err error
				if tab, _, err = openTab(browserCtx, c, url, c.isolate, false); err != nil {
					errs[i] = err
					return
				}
			}
			results[i], errs[i] = renderRange(tab, c, m, from, to, parts[i])
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if n > 1 {
		if err := concat(parts, c.out); err != nil {
			return err
		}
	}
	var total stats
	for _, r := range results {
		total.Frames += r.Frames
		total.Eval += r.Eval
		total.Capture += r.Capture
		total.Damageless += r.Damageless
	}
	fi, _ := os.Stat(c.out)
	summary := map[string]any{
		"out": c.out, "frames": total.Frames, "chunks": n, "capture": c.capture, "encoder": c.encoder,
		"scale": c.scale, "launch_s": launched.Seconds(), "load_s": loaded.Seconds(),
		"eval_s": total.Eval, "capture_s": total.Capture, "no_damage_frames": total.Damageless,
		"wall_s": time.Since(t0).Seconds(), "bytes": fi.Size(),
	}
	b, _ := json.Marshal(summary)
	fmt.Println(string(b))
	return nil
}

// openTab opens a page target, applies the viewport, loads the composition and waits for kurgu.ready.
func openTab(browserCtx context.Context, c config, url string, isolate, first bool) (context.Context, meta, error) {
	var ctx context.Context
	switch {
	case c.capture == "beginframe":
		// BeginFrame control must be requested when the target is created.
		on := true
		res, err := cdp.Call(browserCtx, chromedp.FromContext(browserCtx).Browser, target.CreateTarget,
			target.CreateTargetParams{URL: "about:blank", EnableBeginFrameControl: &on})
		if err != nil {
			return nil, meta{}, fmt.Errorf("create begin-frame target: %w", err)
		}
		ctx, _ = chromedp.NewContext(browserCtx, chromedp.WithTargetID(res.TargetID))
	case isolate:
		ctx, _ = chromedp.NewContext(browserCtx, chromedp.WithNewBrowserContext())
	case first:
		ctx = browserCtx // the initial tab
	default:
		ctx, _ = chromedp.NewContext(browserCtx) // another tab in the default browser context
	}
	var m meta
	err := chromedp.Do(ctx,
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
				Width: 1080, Height: 1920, DeviceScaleFactor: c.scale, Mobile: false,
			})
			if err != nil || !c.bgOverride {
				return err
			}
			_, err = cdp.Call(ctx, t, emulation.SetDefaultBackgroundColorOverride, emulation.SetDefaultBackgroundColorOverrideParams{
				Color: &cdp.RGBA{R: 0, G: 0, B: 0, A: 1},
			})
			return err
		}),
		chromedp.Navigate(url),
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			var err error
			m, err = chromedp.Run(ctx, chromedp.Evaluate[meta](
				`window.kurgu.ready.then(() => ({duration: kurgu.duration, fps: kurgu.fps, width: kurgu.width, height: kurgu.height}))`,
				chromedp.EvalAwaitPromise))
			return err
		}),
	)
	return ctx, m, err
}

func evalAt(ctx context.Context, t float64) error {
	_, err := chromedp.Run(ctx, chromedp.Evaluate[int](
		fmt.Sprintf(`Promise.resolve(kurgu.renderAt(%s)).then(() => 0)`, strconv.FormatFloat(t, 'f', -1, 64)),
		chromedp.EvalAwaitPromise))
	return err
}

func captureFrame(ctx context.Context, c config, f int, fps float64) ([]byte, error) {
	q := int64(c.quality)
	var data []byte
	err := chromedp.Do(ctx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		switch c.capture {
		case "beginframe":
			p := headlessexperimental.BeginFrameParams{
				FrameTimeTicks: 1e6 + float64(f)*1000/fps, Interval: 1000 / fps,
				Screenshot: &headlessexperimental.ScreenshotParams{
					Format: headlessexperimental.ScreenshotParamsFormat(c.format), OptimizeForSpeed: true,
				},
			}
			if c.format == "jpeg" {
				p.Screenshot.Quality = &q
			}
			res, err := cdp.Call(ctx, t, headlessexperimental.BeginFrame, p)
			data = res.ScreenshotData
			return err
		default:
			fast := c.capture == "screenshot"
			p := page.CaptureScreenshotParams{Format: page.CaptureScreenshotFormat(c.format), OptimizeForSpeed: &fast}
			if c.format == "jpeg" {
				p.Quality = &q
			}
			res, err := cdp.Call(ctx, t, page.CaptureScreenshot, p)
			data = res.Data
			return err
		}
	}))
	return data, err
}

func renderRange(ctx context.Context, c config, m meta, from, to int, out string) (stats, error) {
	st := stats{}
	ff := ffmpegCmd(c, m.FPS, out)
	stdin, err := ff.StdinPipe()
	if err != nil {
		return st, err
	}
	if err := ff.Start(); err != nil {
		return st, err
	}
	frames := make(chan []byte, 8)
	writeErr := make(chan error, 1)
	go func() {
		var err error
		for b := range frames {
			if err == nil {
				_, err = stdin.Write(b)
			}
		}
		stdin.Close()
		writeErr <- err
	}()
	var last []byte
	for f := from; f < to; f++ {
		start := time.Now()
		if err := evalAt(ctx, float64(f)/m.FPS); err != nil {
			close(frames)
			return st, fmt.Errorf("renderAt frame %d: %w", f, err)
		}
		mid := time.Now()
		data, err := captureFrame(ctx, c, f, m.FPS)
		if err != nil {
			close(frames)
			return st, fmt.Errorf("capture frame %d: %w", f, err)
		}
		if len(data) == 0 { // beginFrame without damage: the frame is unchanged
			st.Damageless++
			data = last
		}
		last = data
		end := time.Now()
		st.Eval += mid.Sub(start).Seconds()
		st.Capture += end.Sub(mid).Seconds()
		st.Frames++
		frames <- data
		if c.duty > 0 && c.duty < 1 {
			time.Sleep(time.Duration(float64(end.Sub(start)) * (1/c.duty - 1)))
		}
	}
	close(frames)
	if err := <-writeErr; err != nil {
		return st, err
	}
	return st, ff.Wait()
}

func ffmpegCmd(c config, fps float64, out string) *exec.Cmd {
	in := "mjpeg"
	if c.format == "png" {
		in = "png"
	}
	args := []string{"-y", "-loglevel", "error", "-f", "image2pipe", "-framerate", strconv.FormatFloat(fps, 'f', -1, 64), "-c:v", in, "-i", "-"}
	switch c.encoder {
	case "h264_videotoolbox":
		args = append(args, "-c:v", "h264_videotoolbox", "-q:v", "65", "-allow_sw", "1")
	default:
		args = append(args, "-c:v", "libx264", "-preset", c.preset, "-crf", strconv.Itoa(c.crf))
	}
	if c.threads > 0 {
		args = append(args, "-threads", strconv.Itoa(c.threads), "-filter_threads", "1")
	}
	args = append(args, "-pix_fmt", "yuv420p", "-movflags", "+faststart", out)
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd
}

func concat(parts []string, out string) error {
	list := out + ".txt"
	var b strings.Builder
	for _, p := range parts {
		abs, _ := filepath.Abs(p)
		fmt.Fprintf(&b, "file '%s'\n", abs)
	}
	if err := os.WriteFile(list, []byte(b.String()), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", list, "-c", "copy", "-movflags", "+faststart", out)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	for _, p := range append(parts, list) {
		os.Remove(p)
	}
	return nil
}

// snapshots is preview_frame(t): load once, write one PNG per requested time.
func snapshots(browserCtx context.Context, c config, url string, t0 time.Time) error {
	c.format = "png"
	ctx, _, err := openTab(browserCtx, c, url, false, true)
	if err != nil {
		return err
	}
	loaded := time.Since(t0)
	if err := os.MkdirAll(filepath.Dir(c.out), 0o755); err != nil {
		return err
	}
	var files []string
	for i, s := range strings.Split(c.at, ",") {
		t, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return err
		}
		if err := evalAt(ctx, t); err != nil {
			return err
		}
		data, err := captureFrame(ctx, c, i, 30)
		if err != nil {
			return err
		}
		name := fmt.Sprintf("%s_%05.2f.png", c.out, t)
		if err := os.WriteFile(name, data, 0o644); err != nil {
			return err
		}
		files = append(files, name)
	}
	b, _ := json.Marshal(map[string]any{"files": files, "load_s": loaded.Seconds(), "wall_s": time.Since(t0).Seconds()})
	fmt.Println(string(b))
	return nil
}

func allocCtx2(parent context.Context, opts []chromedp.ExecAllocatorOption) context.Context {
	ctx, _ := chromedp.NewExecAllocator(parent, opts...)
	return ctx
}
