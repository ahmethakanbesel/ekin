#!/usr/bin/env bash
# BeginFrame runs in the container (Linux headless shell only). Run from bench/render.
set -u
LOG=${MEASURE_LOG:-out/results.jsonl}
D() { cpus=$1; keep=$2; label=$3; shift 3
  docker run --rm --cpus "$cpus" --memory 1.5g --memory-swap 1.5g --shm-size 1g -e GOMAXPROCS=1 -e KEEP="$keep" \
    -v "$PWD":/bench kurgu-render-bench ./tools/bench-linux.sh "docker cpus=$cpus $label" "$@" | tee -a "$LOG" | cut -c1-200; }
S=(-chrome /usr/bin/chromium-headless-shell -preset veryfast -threads 1 -capture beginframe)
# deviceScaleFactor from Emulation is ignored by beginFrame screenshots; scale with the browser flag instead.
D 1.0 bf-540.mp4 "shell beginframe 540 (force-device-scale-factor=0.5)" "${S[@]}" -chrome-flags force-device-scale-factor=0.5
D 0.5 "" "shell beginframe 540 (force-device-scale-factor=0.5)" "${S[@]}" -chrome-flags force-device-scale-factor=0.5
D 0.5 bf-1080.mp4 "shell beginframe 1080" "${S[@]}"
