#!/usr/bin/env bash
# Inside the container (cwd /bench): one measured vanilla render; prints the cgroup memory peak too.
# usage: tools/bench-linux.sh LABEL driver-args...
set -u
label=$1; shift
B=./vanilla-go/kurgu-render-bench-linux-$(uname -m | sed 's/aarch64/arm64/;s/x86_64/amd64/')
MEASURE_LOG= python3 tools/measure.py "$label" -- "$B" -timeout 20m -out /tmp/out.mp4 "$@" > /tmp/m.json
peak=$(cat /sys/fs/cgroup/memory.peak 2>/dev/null || echo 0)
python3 - "$peak" <<'PY'
import json, sys
r = json.load(open("/tmp/m.json")); r["cgroup_peak_mb"] = round(int(sys.argv[1]) / 2**20)
print(json.dumps(r, ensure_ascii=False))
PY
[ -n "${KEEP:-}" ] && cp /tmp/out.mp4 "out/$KEEP"
exit 0
