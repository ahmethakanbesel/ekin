#!/usr/bin/env bash
# Full 20 s renders on the Mac. Appends one JSON line per run to out/results.jsonl. Run from bench/render.
set -u
export MEASURE_LOG=${MEASURE_LOG:-out/results.jsonl}
B=./vanilla-go/ekin-render-bench
HS=${HEADLESS_SHELL:-$HOME/Library/Caches/ms-playwright/chromium_headless_shell-1243/chrome-headless-shell-mac-arm64/chrome-headless-shell}
FAST=disable-frame-rate-limit,disable-gpu-vsync
N=0; FROM=${FROM:-0}  # FROM=n skips the first n runs
M() { N=$((N+1)); [ $N -le $FROM ] && return; label=$1; shift; python3 tools/measure.py "$label" -- "$@" >/dev/null || echo "FAILED $label"; tail -1 "$MEASURE_LOG" | cut -c1-160; }
V="-preset veryfast"
M "vanilla chrome 1080 x264"             $B $V -out out/v-chrome-1080.mp4
M "vanilla chrome 540 x264"              $B $V -scale 0.5 -out out/v-chrome-540.mp4
M "vanilla shell+fast 1080 x264"         $B $V -chrome "$HS" -chrome-flags $FAST -out out/v-shell-1080.mp4
M "vanilla shell+fast 540 x264"          $B $V -chrome "$HS" -chrome-flags $FAST -scale 0.5 -out out/v-shell-540.mp4
M "vanilla shell+fast 1080 x264 medium"  $B -preset medium -chrome "$HS" -chrome-flags $FAST -out out/v-shell-1080-medium.mp4
M "vanilla shell+fast 1080 vtb"          $B -encoder h264_videotoolbox -chrome "$HS" -chrome-flags $FAST -out out/v-shell-1080-vtb.mp4
M "vanilla shell+fast 1080 vtb chunks4 (4 browsers)"  $B -encoder h264_videotoolbox -chunks 4 -browsers -chrome "$HS" -chrome-flags $FAST -out out/v-shell-1080-vtb-c4.mp4
M "vanilla shell+fast 1080 x264 chunks4 (4 browsers)" $B $V -chunks 4 -browsers -chrome "$HS" -chrome-flags $FAST -out out/v-shell-1080-c4.mp4
M "vanilla chrome 1080 vtb chunks4 (4 tabs)"      $B -encoder h264_videotoolbox -chunks 4 -out out/v-chrome-1080-vtb-c4.mp4
# constrained proxy: 1 Go thread, 1 ffmpeg thread, 1 renderer, background QoS (E-cores only)
export GOMAXPROCS=1
M "constrained shell+fast 1080 x264"     taskpolicy -c background $B $V -threads 1 -chrome "$HS" -chrome-flags $FAST,renderer-process-limit=1 -out out/c-shell-1080.mp4
M "constrained shell+fast 540 x264"      taskpolicy -c background $B $V -threads 1 -chrome "$HS" -chrome-flags $FAST,renderer-process-limit=1 -scale 0.5 -out out/c-shell-540.mp4
M "constrained shell single-process 540" taskpolicy -c background $B $V -threads 1 -chrome "$HS" -chrome-flags $FAST,single-process -scale 0.5 -out out/c-shell-540-sp.mp4
unset GOMAXPROCS
M "vanilla chrome 1080 x264 chunks4 (4 tabs)"     $B $V -chunks 4 -out out/v-chrome-1080-c4.mp4
M "vanilla chrome 1080 vtb chunks8 (8 tabs)"      $B -encoder h264_videotoolbox -chunks 8 -out out/v-chrome-1080-vtb-c8.mp4
M "vanilla chrome 540 vtb chunks4 (4 tabs)"       $B -encoder h264_videotoolbox -chunks 4 -scale 0.5 -out out/v-chrome-540-vtb-c4.mp4
M "vanilla shell 1080 vtb chunks4 (4 tabs)"       $B -encoder h264_videotoolbox -chunks 4 -chrome "$HS" -out out/v-shell-1080-vtb-c4tabs.mp4
