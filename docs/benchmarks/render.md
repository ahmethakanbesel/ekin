# Render engine benchmark

Spike for PLAN.md §2 "Render engine" and §8 milestone 0. The question: which way should Ekin turn an
animated HTML/CSS/JS composition into an MP4?

1. **vanilla**: `composition.html` exposes `window.ekin = { duration, fps, width, height, ready, renderAt(t) }`.
   A Go chromedp driver calls `renderAt(t)` for each frame, captures a JPEG and pipes it to ffmpeg.
2. **Remotion** 4.0.535: the same composition as a React component (`useCurrentFrame`).
3. **Revideo** 0.11.0: the same composition as a Motion Canvas scene.

Code, scripts and a README are in [`bench/render/`](../../bench/render/). Raw numbers are written to
`bench/render/out/results.jsonl`, which is gitignored.

**Recommendation: vanilla.** On Linux, capture frames with `HeadlessExperimental.beginFrame` in
`chrome-headless-shell`. On the Mac, use desktop Chrome with several tabs and VideoToolbox. Details
[below](#recommendation).

## Setup

| | |
|---|---|
| Host | Apple M4, 10 cores (4 performance + 6 efficiency), 16 GB RAM, macOS 15.3 (Darwin 24.3) |
| Browsers | Google Chrome 155.0.8059.40 (desktop, `--headless`); chrome-headless-shell 155 (Playwright's build); Debian chromium and chromium-headless-shell 155.0.8059.39 |
| Encoder | ffmpeg 8.0 (Homebrew) on the Mac, ffmpeg 5.1 in Debian. libx264 `-preset veryfast -crf 20 -pix_fmt yuv420p`, the production default, unless a row says otherwise |
| VPS proxy | OrbStack, `debian:bookworm-slim` with apt `chromium chromium-headless-shell ffmpeg`, arm64. `--cpus N --memory 1.5g --memory-swap 1.5g --shm-size 1g`, `GOMAXPROCS=1`, `ffmpeg -threads 1` |
| Composition | 20 s, 1080×1920, 30 fps (600 frames). Hue-shifting gradient, title slide/fade, 3 eased bullet cards (4 s, 8 s, 12 s), progress bar, karaoke captions over 40 timed Turkish words in pages of 5, a local PNG with a Ken Burns zoom. Bundled Inter font. No network |
| Measured | Wall clock for the whole command (launch, load, render, encode, mux). Peak RSS is the sum over the process tree, sampled every 200 ms. On macOS this double-counts shared pages. In Docker, the cgroup `memory.peak` is also given and is the more honest number |

Preview scale 0.5 (540×960) keeps the 1080×1920 CSS layout and sets `deviceScaleFactor: 0.5`. BeginFrame is
the exception: it ignores the emulated scale factor, so it uses `--force-device-scale-factor=0.5` instead
(see the notes on the driver).

## Results

### Mac (M4)

| Engine / configuration | 1080×1920 | 540×960 | Peak RSS (tree) | Output 1080 / 540 |
|---|---:|---:|---:|---:|
| vanilla Go, desktop Chrome, `captureScreenshot`, 1 tab | 33.0 s | 31.2 s | 1.5 GB | 5.9 MB / 1.3 MB |
| vanilla Go, headless shell + `--disable-frame-rate-limit --disable-gpu-vsync`, 1 tab | **14.2 s** | **5.2 s** | 0.9 / 0.5 GB | 5.9 MB / 1.3 MB |
| same, x264 `-preset medium` | 13.9 s | not measured | 1.1 GB | 7.0 MB |
| same, `h264_videotoolbox -q:v 65` | 13.7 s | not measured | 0.5 GB | 14.9 MB |
| vanilla Go, desktop Chrome, 4 tabs (`render.chunks=4`), x264 | 10.2 s | not measured | 3.8 GB | 5.9 MB |
| vanilla Go, desktop Chrome, 4 tabs, VideoToolbox | 9.5 s | 8.6 s | 2.7 GB | 15.0 MB / 4.4 MB |
| vanilla Go, desktop Chrome, 8 tabs, VideoToolbox | **9.0 s** | not measured | 4.2 GB | 15.2 MB |
| vanilla Go, headless shell (no extra flags), 4 tabs, VideoToolbox | 9.7 s | not measured | 1.5 GB | 15.0 MB |
| vanilla Go, headless shell + fast flags, 4 **browsers**, VideoToolbox / x264 | 55 s / 85 s | not measured | 1.9 / 3.2 GB | ⚠ pathological, see notes |
| Remotion, default concurrency, `--image-format jpeg --x264-preset veryfast --crf 20` | 10.8 s | not measured | 2.2 GB | 4.5 MB |
| Remotion, `--concurrency 1` | not measured | 32.1 s | 1.1 GB | 1.4 MB |
| Revideo, 1 worker (canvas → PNG → libx264, CRF 23 is fixed) | ~15.9 s ¹ | not measured | 1.6 GB | 3.4 MB |
| Playwright driver (`bench/render/vanilla-pw`) | not measured ² | not measured | | |

¹ The video track finished encoding (601 frames, complete). Revideo's final audio/merge step then crashed
with ffmpeg 8 (fluent-ffmpeg reports "Input format lavfi is not available"). Run with its bundled ffmpeg 4.4,
it hung before rendering. The time is measured to that crash.
² The script is written but was not timed. Playwright drives the same `Page.captureScreenshot` as chromedp,
so it should match the chromedp rows.

**Constrained Mac proxy.** `taskpolicy -c background` keeps the job on the efficiency cores. `GOMAXPROCS=1`,
`ffmpeg -threads 1`, `--renderer-process-limit=1`, headless shell with the fast flags:

| | 1080 | 540 | Peak RSS |
|---|---:|---:|---:|
| shell + fast flags | 63.4 s | 25.1 s | 585 / 438 MB |
| shell + fast flags + `--single-process` | not measured | 25.5 s | 313 MB |
| desktop Chrome + `--single-process` | fails to start | fails to start | |

### Linux container (VPS proxy), vanilla Go driver

| `--cpus` | Capture | 540×960 | 1080×1920 | cgroup peak (540 / 1080) |
|---:|---|---:|---:|---:|
| 2.0 | screenshot + fast flags, ffmpeg threads auto | not measured | 21.6 s | — / 639 MB |
| 1.0 | headless shell, `captureScreenshot`, no extra flags | 30.0 s | not measured | 318 MB / — |
| 1.0 | headless shell, screenshot + fast flags | 32.8 s | 50.1 s | 654 / 515 MB |
| 1.0 | chromium (new headless), screenshot + fast flags | 59.2 s | not measured | 617 MB / — |
| 1.0 | **headless shell, `beginFrame`** | **9.5 s** | **20.3 s** | 601 / 583 MB |
| 0.5 | headless shell, screenshot + fast flags | 136.7 s | 254.0 s | 455 / 515 MB |
| 0.5 | **headless shell, `beginFrame`** | **19.1 s** | **76.6 s** | 199 ³ / 523 MB |
| 0.25 | headless shell, screenshot + fast flags | 340.2 s | not measured | 427 MB / — |

³ This low reading could not be explained. The other runs peaked at 450–650 MB, including page cache.

Neither Remotion nor Revideo was run in the container: not measured.

### Capture microbenchmark (90 frames, 1080×1920, per-frame capture time)

| Where | Browser | Capture | ms / frame |
|---|---|---|---:|
| Mac | desktop Chrome | `Page.captureScreenshot` (jpeg 90, `optimizeForSpeed` true or false) | 49 |
| Mac | desktop Chrome | + `--disable-frame-rate-limit` | hangs on frame 2 |
| Mac | desktop Chrome | `--gpu` (no `--disable-gpu`) | 66 |
| Mac | desktop Chrome | PNG instead of JPEG | 60 |
| Mac | headless shell | screenshot | 50 |
| Mac | headless shell | screenshot + `--disable-frame-rate-limit --disable-gpu-vsync` | 23 (8 at 540) |
| Mac | headless shell | `beginFrame` | not supported: "BeginFrameControl is not supported on MacOS yet" |
| Mac | desktop Chrome | `HeadlessExperimental.beginFrame` | method not found (new headless) |
| Linux, unlimited CPU | chromium (new headless) | screenshot / + fast flags | 53 / 22 |
| Linux, unlimited CPU | headless shell | screenshot / + fast flags | 51 / 24 |
| Linux, unlimited CPU | headless shell | `beginFrame` | 26 |

`renderAt(t)` itself costs 0.3–1.5 ms a frame. The cost is all in capture.

### Visual check

Frame 198 (t = 6.6 s) was extracted from each 1080p MP4 and compared with the vanilla frame (the
contact sheet is `bench/render/out/snap/cmp-engines.jpg`):

- **Remotion vs vanilla**: visually the same (PSNR 38 dB, x264 noise). A Remotion still against a vanilla PNG
  snapshot at the same time: 63.7 dB, which is pixel-identical apart from rounding.
- **Linux beginFrame vs Mac vanilla**: 31 dB. The difference is font rasterisation between Linux and macOS,
  not timing. Within one platform, every capture method gives a **byte-identical MP4**: screenshot, screenshot
  + fast flags and beginFrame all came to 5,214,922 bytes in Linux, and 4,223,448 bytes on the Mac for the
  90-frame test.
- **Revideo**: the gradient, title, image, Ken Burns and cards match, but **the karaoke captions did not
  render** (reactive `Txt` children inside a flex `Layout`). The title also sits a few px lower because canvas
  text metrics differ from CSS. This was not debugged further. The fact that it failed silently is a finding
  in itself.

## Authoring by an unattended LLM agent

| | vanilla | Remotion | Revideo |
|---|---|---|---|
| Model | Plain DOM/CSS. `renderAt(t)` sets styles from `t` | React, styles from `useCurrentFrame()`. The same pure-function idea | Motion Canvas scene graph on a canvas. Generator timelines (`yield* tween`) and signals. Not HTML/CSS |
| Training-data familiarity | Highest (HTML/CSS/JS) | High (React + a small API) | Low (niche API, own layout and text engine) |
| Ease of writing correctly | Easy. One file, no build | Easy, but needs a bundler, `staticFile`, `delayRender` for fonts | Hard. Our own port needed coordinate maths (centre origin) and still broke captions without an error |
| Validation | Load the page, check the `ekin` contract, `renderAt` at sampled t, catch console errors, block network (`--host-resolver-rules`) | `remotion compositions` + render stills | Vite build + render. Errors surface late |
| `preview_frame(t)` | **1.3 s cold** (Chrome launch + load + PNG), single-digit ms per frame warm | `remotion still`: 15 s first (bundling), 2 s cached | Requires the Vite dev server + puppeteer. Not timed |
| Determinism | Bundled font, local server, network blocked, byte-identical across capture modes | Same (Chromium) | Canvas text depends on the font being loaded first. Must `yield document.fonts.load()` |
| Install weight | Go binary 11 MB + a system Chromium. Debian image with chromium, headless shell and ffmpeg: 1.19 GB | 390 MB node_modules + 570 MB headless shell it downloads on the first render (needs network) | 263 MB node_modules + puppeteer Chrome (362 MB) + headless shell (201 MB) in `~/.cache/puppeteer`. Telemetry is on unless `DISABLE_TELEMETRY=true` |
| Runtime | Go + Chromium + ffmpeg (already in our stack) | Node + its own Chromium + its compositor binaries | Node + Vite + puppeteer. Forces `--single-process`, which desktop Chrome on macOS cannot run |
| License | Ours | **Source-available, not OSS.** Free for individuals, non-profits and for-profit orgs **up to 3 employees**. Above that, a Company License: "Remotion for Automators", for apps and systems that render (our case), at **$0.01 / render, $100 / month minimum**. Creators: $25 / seat / month. Enterprise from $500 / month (remotion.pro/license, checked 2026-10-14). The license text says it "will slightly change" in Remotion 5.0 | MIT. Last release 0.11.0 (July 2026). The API has gaps (the ffmpeg 8 crash, no CRF setting) |

## Observations

1. **The capture method matters more than the engine.** All three engines are Chromium screenshot loops.
   Remotion's 10.8 s at 1080p comes from parallel tabs. With one tab at 540p it takes 32 s, the same as
   our desktop-Chrome baseline, because it waits for the same ~50 ms frame cadence.
2. **Default `captureScreenshot` costs about 50 ms a frame, whatever the resolution.** It waits for Chrome's
   60 Hz compositor schedule, so 540p is barely faster than 1080p (31 s vs 33 s). The capture is timing-bound,
   not pixel-bound.
3. **`--disable-frame-rate-limit --disable-gpu-vsync`** removes that wait and gives 2–6× on an idle machine
   (Mac headless shell: 14 s at 1080, 5 s at 540). But it makes the compositor spin. On a capped CPU it is
   no faster or slower: 30 s → 33 s at 1 vCPU, and 59 s with new-headless chromium. It also **hangs when a
   second tab or a second browser competes**: two tabs deadlock, and four separate browsers take 55–85 s. On
   desktop Chrome (macOS) it hangs outright.
4. **`HeadlessExperimental.beginFrame` is the clear winner where it works** (Linux, chrome-headless-shell
   only). The driver issues frames itself, so nothing spins and nothing waits for vsync. At 1 vCPU: 540p in
   9.5 s and 1080p in 20.3 s. At 0.5 vCPU: 540p in 19 s and 1080p in 77 s. Screenshot mode takes 137 s and
   254 s for the same jobs. Its output is byte-identical to screenshot mode.
5. **VPS sizing (cpus 0.5, PLAN.md §4 ceiling) with beginFrame**: a 20 s short previews in about 1× real time
   at 540p and finals in about 4× real time at 1080p. A 60 s short takes about 1 min to preview and 4 min for
   the final. Memory stays well inside the 1.5 GB cap: cgroup peak about 0.5–0.6 GB.
6. **Mac**: the best wall time comes from parallel tabs with desktop Chrome and VideoToolbox (9 s for 20 s of
   1080p, about 2.2× real time). Going from 4 to 8 tabs barely helps. VideoToolbox at `-q:v 65` is 2.5× larger
   than x264 CRF 20 and barely faster here, because capture dominates. Keep x264 for finals unless file size
   doesn't matter. For one-tab previews, the headless shell with the fast flags is fastest (5 s for 540p).
7. **x264 preset**: `veryfast` and `medium` take the same wall time (capture-bound) but medium costs about 80%
   more CPU. `veryfast` is the right default on the VPS.
8. `optimizeForSpeed`, `setDefaultBackgroundColorOverride` and JPEG quality 80 vs 90 made no measurable
   difference. PNG frames are about 20% slower. Enabling the GPU on the Mac is slower (66 ms).
9. `--single-process` works with the headless shell (25.5 s, 313 MB, the lowest memory) and fails with
   desktop Chrome.

## Recommendation

**Use vanilla (`composition.html` + `renderAt(t)` + the Go chromedp driver + system ffmpeg).**

- It is fastest where it matters, on the constrained VPS with beginFrame: 3–7× faster than screenshot capture
  at 0.5 vCPU. It is as fast as Remotion on the Mac with tabs.
- An agent can write it easily and check it cheaply. `preview_frame(t)` is a 1.3 s cold call and milliseconds
  warm, with no bundler and no build step.
- There is no license exposure. Remotion would need a Company License once the org has more than 3 people,
  and Ekin is exactly the "Automators" case ($100 / month minimum).
- It is the lightest dependency: one Go binary plus Chromium and ffmpeg, which are already in the stack. No
  Node in the render path.
- It is deterministic: bundled fonts, a local file server, network blocked, and identical frames from every
  capture mode.

Rejected: **Remotion** is good and nearly equivalent visually, but it brings licensing, a Node + bundler +
downloaded-browser runtime, and gives no speed advantage. **Revideo** is canvas-based rather than HTML/CSS,
harder for an agent to author, and in this spike it dropped the captions without an error and crashed on
ffmpeg 8.

## Notes for the production renderer (`internal/adapter/render/render.go`)

It already matches the benchmark driver: screenshot capture, a new browser context per chunk, a duty cycle,
`veryfast`/CRF 20, and the same determinism flags. Suggested changes:

1. **Add a beginFrame capture mode for Linux and make it the VPS default.** Use `chrome-headless-shell`
   (Debian: `apt install chromium-headless-shell` → `/usr/bin/chromium-headless-shell`, or the
   `chromedp/headless-shell` image).
   - Extra launch flags: `--enable-begin-frame-control --deterministic-mode`
     (deterministic-mode also enables `--run-all-compositor-stages-before-draw` and disables threaded
     animation and scrolling).
   - Create the page target yourself with begin-frame control, then attach:
     ```go
     on := true
     res, _ := cdp.Call(browserCtx, chromedp.FromContext(browserCtx).Browser, target.CreateTarget,
         target.CreateTargetParams{URL: "about:blank", EnableBeginFrameControl: &on})
     tab, _ := chromedp.NewContext(browserCtx, chromedp.WithTargetID(res.TargetID))
     ```
   - For each frame: `Runtime.evaluate("Promise.resolve(ekin.renderAt(t)).then(() => 0)", awaitPromise)`, then
     ```go
     cdp.Call(ctx, t, headlessexperimental.BeginFrame, headlessexperimental.BeginFrameParams{
         FrameTimeTicks: 1e6 + float64(f)*1000/fps, Interval: 1000 / fps,
         Screenshot: &headlessexperimental.ScreenshotParams{Format: "jpeg", Quality: &q90, OptimizeForSpeed: true},
     })
     ```
     If `ScreenshotData` is empty (no damage), reuse the previous frame. This never happened in 600-frame runs.
   - **Scale:** beginFrame screenshots ignore `Emulation.setDeviceMetricsOverride`'s `deviceScaleFactor`.
     For 540p, launch with `--force-device-scale-factor=0.5` and keep `--window-size=1080,1920`. The scale is
     then fixed per browser, so use one browser per scale.
   - It is not supported on macOS, so fall back to screenshot there. Detect it with
     `-32000 BeginFrameControl is not supported` or by GOOS.
   - Not measured: beginFrame with several chunks (one begin-frame target per chunk). With 1 vCPU,
     `render.chunks = 1` is right anyway.
2. **Mac profile:** keep screenshot mode with desktop Chrome and one new browser context per tab, with
   `render.chunks = 4` (8 adds about 5%). Use `h264_videotoolbox` only for previews, and x264 for finals
   (VideoToolbox `-q:v 65` gives 2.5× bigger files).
3. **Do not use `--disable-frame-rate-limit` / `--disable-gpu-vsync` by default.** They help only a single
   tab on an idle machine with the headless shell. They hang desktop Chrome on macOS and multi-tab or
   multi-browser runs, and are slower on capped CPUs. If you want the fastest single-tab Mac preview (5 s for
   540p), make it an opt-in for `chunks = 1` with the headless shell.
4. **Add a timeout around every CDP call** (for example, 30 s per frame). Hangs are silent: chromedp blocks
   forever when the compositor stops producing frames.
5. Keep `ffmpeg -threads 1` and `GOMAXPROCS=1` on the VPS. Encode is not the bottleneck: capture is about 90%
   of wall time in every capped run.
6. `--single-process` is a valid low-memory option for the headless shell (313 MB). Desktop Chrome cannot
   start with it.
7. Watch out with `render.preview_scale` in screenshot mode: 540p is barely faster than 1080p (the ~50 ms
   cadence). The preview speed-up only appears with beginFrame or the fast flags.
