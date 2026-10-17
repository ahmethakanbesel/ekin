# Render engine benchmark

The same 20 s, 1080×1920, 30 fps composition built three ways. Results and the recommendation are in
[`docs/benchmarks/render.md`](../../docs/benchmarks/render.md).

```
assets/        Inter.ttf (OFL, Google Fonts) and photo.png (made with ffmpeg's mandelbrot source)
shared/        spec.json: title, bullets, 40 word timings. Remotion and Revideo import it; vanilla inlines it
vanilla/       composition.html: window.ekin = { duration, fps, width, height, ready, renderAt(t) }
vanilla-go/    chromedp + ffmpeg driver (own go.mod), the prototype of internal/adapter/render
vanilla-pw/    playwright-core, installed for comparison only (prior art: clinic-saas videos/*/tools/render.mjs)
remotion/      the composition as a React component (useCurrentFrame)
revideo/       the composition as a Revideo (Motion Canvas) scene
docker/        Debian bookworm + chromium + chromium-headless-shell + ffmpeg (VPS proxy)
tools/         measure.py (wall time, peak RSS of the process tree), bench-*.sh, capture-matrix*.sh
out/           renders, snapshots, results.jsonl (gitignored)
```

## Setup

```sh
cd bench/render
(cd vanilla-go && go build -o ekin-render-bench . \
  && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ekin-render-bench-linux-arm64 .)
(cd remotion && npm install)     # first render downloads chrome-headless-shell into node_modules/.remotion
(cd revideo && npm install)      # puppeteer downloads Chrome + headless shell into ~/.cache/puppeteer
docker build -t ekin-render-bench docker/
```

A macOS chrome-headless-shell is needed for the fast capture path on the Mac. Any copy works; the scripts
default to Playwright's (`npx playwright install chromium-headless-shell`) and accept `HEADLESS_SHELL=/path`.

## Run

```sh
# preview_frame(t): PNG snapshots, about 1.3 s cold including Chrome launch
./vanilla-go/ekin-render-bench -at 0.5,4.3,12.6 -out out/snap/vanilla

# one full render
./vanilla-go/ekin-render-bench -preset veryfast -out out/vanilla.mp4
./vanilla-go/ekin-render-bench -chrome "$HEADLESS_SHELL" \
  -chrome-flags disable-frame-rate-limit,disable-gpu-vsync -scale 0.5 -out out/vanilla-540.mp4

./tools/capture-matrix.sh        # 90-frame capture comparison on the Mac
docker run --rm --shm-size 1g -v "$PWD":/bench ekin-render-bench ./tools/capture-matrix-linux.sh
./tools/bench-mac.sh             # full renders on the Mac      -> out/results.jsonl
./tools/bench-docker.sh          # --cpus 1.0 / 0.5 / 0.25      -> out/results.jsonl
./tools/bench-docker-beginframe.sh  # beginFrame capture (Linux headless shell only)
./tools/bench-js.sh              # Remotion and Revideo          -> out/results.jsonl
```

Driver flags: `-capture screenshot|screenshot-slow|beginframe`, `-scale`, `-encoder libx264|h264_videotoolbox`,
`-preset`, `-crf`, `-threads`, `-chunks N` (`-browsers` = one Chrome per chunk, `-isolate` = one browser context
per tab), `-duty`, `-chrome`, `-chrome-flags a,b=c`, `-frames N`, `-timeout`.

Remotion: `cd remotion && npx remotion render src/index.ts Bench ../out/remotion.mp4 --public-dir ../assets`.
Revideo: `cd revideo && DISABLE_TELEMETRY=true node render.mjs [--half] [--workers N]`.
