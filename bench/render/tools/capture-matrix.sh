#!/usr/bin/env bash
# Quick 90-frame comparison of capture options for the Go chromedp driver (run from bench/render).
set -u
B=./vanilla-go/kurgu-render-bench
HS=${HEADLESS_SHELL:-$HOME/Library/Caches/ms-playwright/chromium_headless_shell-1243/chrome-headless-shell-mac-arm64/chrome-headless-shell}
FR=disable-frame-rate-limit,disable-gpu-vsync
run() { echo "== $*"; "$B" -timeout 60s -frames 90 -preset ultrafast -out out/cap.mp4 "$@" 2>&1 | tail -1 | cut -c1-220; }
run -capture screenshot
run -capture screenshot -chrome-flags "$FR"
run -capture screenshot -chrome-flags disable-frame-rate-limit
run -capture screenshot -chrome "$HS"
run -capture screenshot -chrome "$HS" -chrome-flags "$FR"
run -capture screenshot -chrome "$HS" -chrome-flags "$FR" -scale 0.5
run -capture beginframe -chrome "$HS"
