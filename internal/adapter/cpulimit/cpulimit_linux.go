//go:build linux

package cpulimit

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// clockTicks is USER_HZ, the unit of /proc CPU times: 100 on every
// Linux architecture Ekin runs on.
const clockTicks = 100

// Supported reports whether this OS can limit job processes.
func Supported() bool { return true }

// sampler reads the job processes' CPU time and pauses or continues them.
type sampler struct {
	self    int
	last    map[int]uint64 // ticks per pid at the previous sample
	stopped map[int]bool   // pids this limiter paused
}

func newSampler() *sampler {
	return &sampler{self: os.Getpid(), last: map[int]uint64{}, stopped: map[int]bool{}}
}

func (s *sampler) procs() []procStat { return jobProcs(s.self) }

// jobProcs returns the processes below root, with their CPU time.
func jobProcs(root int) []procStat {
	paths, _ := filepath.Glob("/proc/[0-9]*/stat")
	out := make([]procStat, 0, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p) //nolint:gosec // /proc entries
		if err != nil {
			continue // exited meanwhile
		}
		if st, ok := parseStat(string(raw)); ok {
			out = append(out, st)
		}
	}
	return descendants(out, root)
}

// renice sets every thread of pid to nice n: on Linux the priority is
// per thread, and threads started before keep theirs.
func renice(pid, n int) {
	tids, _ := filepath.Glob("/proc/" + strconv.Itoa(pid) + "/task/[0-9]*")
	for _, t := range tids {
		if tid, err := strconv.Atoi(filepath.Base(t)); err == nil {
			_ = syscall.Setpriority(syscall.PRIO_PROCESS, tid, n) // fails only for a higher priority or a gone thread
		}
	}
}

// NiceSupported reports whether this OS can lower job processes' priority.
func NiceSupported() bool { return true }

// sample returns the CPU seconds the job processes used since the last
// sample. A process first seen counts its whole run so far: it started
// within the last period, or this is the baseline sample.
func (s *sampler) sample() float64 {
	now := map[int]uint64{}
	var used uint64
	for _, p := range s.procs() {
		now[p.pid] = p.ticks
		if p.ticks > s.last[p.pid] {
			used += p.ticks - s.last[p.pid]
		}
	}
	s.last = now
	return float64(used) / clockTicks
}

func (s *sampler) pause() {
	for pid := range s.last {
		if !s.stopped[pid] && syscall.Kill(pid, syscall.SIGSTOP) == nil {
			s.stopped[pid] = true
		}
	}
}

func (s *sampler) resume() {
	for pid := range s.stopped {
		_ = syscall.Kill(pid, syscall.SIGCONT)
		delete(s.stopped, pid)
	}
}
