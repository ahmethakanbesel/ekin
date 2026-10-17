#!/usr/bin/env bash
# Capture options inside the Linux container (run via docker, cwd /bench).
set -u
B=./vanilla-go/ekin-render-bench-linux-$(uname -m | sed 's/aarch64/arm64/;s/x86_64/amd64/')
FR=disable-frame-rate-limit,disable-gpu-vsync
run() { echo "== $*"; "$B" -timeout 90s -frames 90 -preset ultrafast -out /tmp/cap.mp4 "$@" 2>&1 | tail -1 | cut -c1-230; }
run -chrome /usr/bin/chromium -capture screenshot
run -chrome /usr/bin/chromium -capture screenshot -chrome-flags "$FR"
run -chrome /usr/bin/chromium-headless-shell -capture screenshot
run -chrome /usr/bin/chromium-headless-shell -capture screenshot -chrome-flags "$FR"
run -chrome /usr/bin/chromium-headless-shell -capture beginframe
run -chrome /usr/bin/chromium-headless-shell -capture beginframe -scale 0.5
run -chrome /usr/bin/chromium-headless-shell -capture screenshot -chrome-flags "$FR" -scale 0.5
