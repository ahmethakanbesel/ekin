#!/usr/bin/env bash
# Remotion and Revideo full renders on the Mac. Appends to out/results.jsonl. Run from bench/render.
set -u
export MEASURE_LOG=${MEASURE_LOG:-out/results.jsonl}
M() { label=$1; shift; python3 ../tools/measure.py "$label" -- "$@" >/dev/null || echo "FAILED $label"; tail -1 "$MEASURE_LOG" | cut -c1-160; }
cd remotion
export MEASURE_LOG=../$MEASURE_LOG
R="npx remotion render src/index.ts Bench --public-dir ../assets --codec h264 --crf 20 --x264-preset veryfast --image-format jpeg --jpeg-quality 90 --log error"
M "remotion 1080 default concurrency" $R ../out/remotion-1080.mp4
M "remotion 540 concurrency 1"        $R --concurrency 1 --scale 0.5 ../out/remotion-540-c1.mp4
[ -n "${SKIP_REVIDEO:-}" ] && exit 0
cd ../revideo
# Revideo: the final audio/merge step fails with ffmpeg 8 (fluent-ffmpeg cannot parse `-formats`); the
# frame render + encode finishes first and leaves visuals.mp4 in $TMPDIR/revideo-*.
export DISABLE_TELEMETRY=true FFMPEG_PATH=$(command -v ffmpeg) FFPROBE_PATH=$(command -v ffprobe)
M "revideo 1080 workers 1" node render.mjs
M "revideo 540 workers 1"  node render.mjs --half
