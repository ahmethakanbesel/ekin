#!/usr/bin/env python3
"""Run a command, sample the RSS of its whole process tree, print a JSON line.

usage: tools/measure.py LABEL -- cmd args...
Output: {"label", "wall_s", "cpu_s", "peak_rss_mb", "exit", "stdout_tail"}
cpu_s is user+sys of all reaped descendants (RUSAGE_CHILDREN).
"""
import json, os, resource, subprocess, sys, time

def tree_rss_kb(root):
    out = subprocess.run(["ps", "-A", "-o", "pid=,ppid=,rss="], capture_output=True, text=True).stdout
    kids, rss = {}, {}
    for line in out.splitlines():
        p = line.split()
        if len(p) != 3:
            continue
        pid, ppid, r = map(int, p)
        kids.setdefault(ppid, []).append(pid)
        rss[pid] = r
    total, stack = 0, [root]
    while stack:
        pid = stack.pop()
        total += rss.get(pid, 0)
        stack.extend(kids.get(pid, []))
    return total

def main():
    label = sys.argv[1]
    cmd = sys.argv[sys.argv.index("--") + 1:]
    t0 = time.time()
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, text=True)
    peak = 0
    while proc.poll() is None:
        peak = max(peak, tree_rss_kb(proc.pid))
        time.sleep(0.2)
    out = proc.stdout.read()
    wall = time.time() - t0
    ru = resource.getrusage(resource.RUSAGE_CHILDREN)
    res = {"label": label, "wall_s": round(wall, 2), "cpu_s": round(ru.ru_utime + ru.ru_stime, 1),
           "peak_rss_mb": round(peak / 1024), "exit": proc.returncode,
           "stdout_tail": out.strip().splitlines()[-1:] }
    line = json.dumps(res, ensure_ascii=False)
    print(line, flush=True)
    log = os.environ.get("MEASURE_LOG")
    if log:
        with open(log, "a") as f:
            f.write(line + "\n")
    sys.exit(proc.returncode)

if __name__ == "__main__":
    main()
