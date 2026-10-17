// Package cpulimit governs the job processes' use of the CPU. The job
// processes are this process's descendants (Claude Code, Chrome, ffmpeg,
// the speech scripts, pdftotext); the Ekin process itself is never
// touched, so the web UI and job leases stay responsive.
//
// Limiter keeps them under a share of the host's CPUs: when together they
// use more than their share they are paused (SIGSTOP) until the share
// has caught up, then continued (SIGCONT), the way cpulimit(1) works.
// Supported on Linux, where the VPS runs; elsewhere Run returns at once.
//
// Nicer (nice.go) lowers their scheduling priority, on Linux and macOS.
package cpulimit

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

// Limiter applies a CPU limit read from the host profile.
type Limiter struct {
	// Percent is the limit in percent of the host's CPUs; 0 or 100 and
	// above mean no limit. It is read every period, so changes apply live.
	Percent func(ctx context.Context) int
	// Changed, when set, returns a channel closed at the next profile
	// change, to apply a new limit without waiting for the idle poll.
	Changed func() <-chan struct{}
	// CPUs is the host's CPU count; 0 means GOMAXPROCS, which follows a
	// container's CPU quota.
	CPUs int
	// Period is the sampling period; 0 means 100ms.
	Period time.Duration
	Log    *slog.Logger
}

// idlePoll is how often an unlimited host re-reads its limit.
const idlePoll = 2 * time.Second

// Run applies the limit until ctx ends, then continues every process it
// paused.
func (l *Limiter) Run(ctx context.Context) {
	if !Supported() {
		return
	}
	period := l.Period
	if period <= 0 {
		period = 100 * time.Millisecond
	}
	cpus := l.CPUs
	if cpus <= 0 {
		cpus = runtime.GOMAXPROCS(0)
	}
	s := newSampler()
	defer s.resume()
	var (
		b      bucket
		paused bool
		active bool // limiting, as of the last period
		last   time.Time
	)
	for ctx.Err() == nil {
		changed := l.changed()
		pct := l.Percent(ctx)
		if pct <= 0 || pct >= 100 {
			if active {
				s.resume()
				l.Log.InfoContext(ctx, "cpu limit off")
			}
			active, paused, b = false, false, bucket{}
			wait(ctx, idlePoll, changed)
			continue
		}
		if !active {
			s.sample() // a baseline: CPU used before the limit does not count
			last, active = time.Now(), true
			l.Log.InfoContext(ctx, "cpu limit on", slog.Int("percent", pct), slog.Int("cpus", cpus))
		}
		wait(ctx, period, nil)
		now := time.Now()
		share := float64(pct) / 100 * float64(cpus) // CPUs' worth
		pause := b.step(share*now.Sub(last).Seconds(), s.sample(), share*2*period.Seconds())
		last = now
		switch {
		case pause:
			s.pause() // also any process started since the last pause
		case paused:
			s.resume()
		}
		paused = pause
	}
}

func (l *Limiter) changed() <-chan struct{} {
	if l.Changed == nil {
		return nil
	}
	return l.Changed()
}

// bucket holds the CPU time (in seconds) the job processes may still
// use: it fills at the allowed rate, up to a small burst, and drains by
// what they used. Below zero they are paused, so their average use
// converges to the allowed rate.
type bucket struct {
	level float64
}

// step adds allowed, takes used, caps at burst, and reports whether the
// processes must pause.
func (b *bucket) step(allowed, used, burst float64) bool {
	b.level = min(b.level+allowed-used, burst)
	return b.level < 0
}

func wait(ctx context.Context, d time.Duration, changed <-chan struct{}) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	case <-changed:
	}
}
