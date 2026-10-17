//go:build linux

package cpulimit_test

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ekin/internal/adapter/cpulimit"
)

// cpuSeconds reads a process's user+system time.
func cpuSeconds(t *testing.T, pid int) float64 {
	t.Helper()
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(string(raw)[strings.LastIndexByte(string(raw), ')')+1:])
	u, _ := strconv.ParseFloat(f[11], 64)
	s, _ := strconv.ParseFloat(f[12], 64)
	return (u + s) / 100
}

// A busy child limited to 20% of one CPU uses about a fifth of the time;
// lifting the limit lets it run free again, live.
func TestLimiterCapsABusyChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "while :; do :; done")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	var pct atomic.Int64
	pct.Store(20)
	l := &cpulimit.Limiter{
		Percent: func(context.Context) int { return int(pct.Load()) },
		CPUs:    1, Period: 50 * time.Millisecond, Log: slog.New(slog.DiscardHandler),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()

	time.Sleep(500 * time.Millisecond) // settle
	before := cpuSeconds(t, cmd.Process.Pid)
	time.Sleep(3 * time.Second)
	limited := (cpuSeconds(t, cmd.Process.Pid) - before) / 3
	if limited < 0.1 || limited > 0.32 {
		t.Fatalf("limited child used %.2f of a CPU, want about 0.20", limited)
	}

	pct.Store(0)
	time.Sleep(idleWait)
	before = cpuSeconds(t, cmd.Process.Pid)
	time.Sleep(time.Second)
	if free := cpuSeconds(t, cmd.Process.Pid) - before; free < 0.7 {
		t.Fatalf("after lifting the limit the child used %.2f of a CPU", free)
	}

	pct.Store(20)
	time.Sleep(idleWait)
	cancel()
	<-done
	// Stopping the limiter continues what it paused.
	before = cpuSeconds(t, cmd.Process.Pid)
	time.Sleep(time.Second)
	if free := cpuSeconds(t, cmd.Process.Pid) - before; free < 0.7 {
		t.Fatalf("child left paused after the limiter stopped: %.2f", free)
	}
}

// idleWait outlasts the limiter's idle poll, so a new limit is in force.
const idleWait = 2500 * time.Millisecond
