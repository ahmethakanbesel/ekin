#!/usr/bin/env bash
# VPS proxy runs: Debian bookworm + distro chromium-headless-shell + ffmpeg in OrbStack/Docker,
# capped with --cpus, mem 1.5g like PLAN.md §4. Appends to out/results.jsonl. Run from bench/render.
set -u
LOG=${MEASURE_LOG:-out/results.jsonl}
FAST=disable-frame-rate-limit,disable-gpu-vsync
D() { cpus=$1; label=$2; shift 2
  docker run --rm --cpus "$cpus" --memory 1.5g --memory-swap 1.5g --shm-size 1g -e GOMAXPROCS=1 -e KEEP="${KEEP:-}" \
    -v "$PWD":/bench kurgu-render-bench ./tools/bench-linux.sh "docker cpus=$cpus $label" "$@" | tee -a "$LOG" | cut -c1-200; }
S="-chrome /usr/bin/chromium-headless-shell -preset veryfast -threads 1"
for cpus in 1.0 0.5; do
  D $cpus "shell+fast 540 x264"  $S -chrome-flags $FAST -scale 0.5
  D $cpus "shell+fast 1080 x264" $S -chrome-flags $FAST
done
D 1.0 "shell (no fast flags) 540 x264" $S -scale 0.5
D 1.0 "chromium+fast 540 x264" -chrome /usr/bin/chromium -preset veryfast -threads 1 -chrome-flags $FAST -scale 0.5
D 1.0 "shell beginframe 1080 x264" $S -capture beginframe
D 2.0 "shell+fast 1080 x264 threads auto" -chrome /usr/bin/chromium-headless-shell -preset veryfast -chrome-flags $FAST
D 0.25 "shell+fast 540 x264" $S -chrome-flags $FAST -scale 0.5
