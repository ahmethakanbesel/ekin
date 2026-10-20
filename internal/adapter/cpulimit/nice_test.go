//go:build linux || darwin

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

	"github.com/ahmethakanbesel/ekin/internal/adapter/cpulimit"
	"github.com/ahmethakanbesel/ekin/pkg/broadcast"
)

// niceOf reads a process's nice value with ps.
func niceOf(t *testing.T, pid int) int {
	t.Helper()
	out, err := exec.Command("ps", "-o", "nice=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("ps nice %q: %v", out, err)
	}
	return n
}

func waitNice(t *testing.T, pid, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for niceOf(t, pid) != want {
		if time.Now().After(deadline) {
			t.Fatalf("nice of %d = %d, want %d", pid, niceOf(t, pid), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Job processes, grandchildren included, drop to the profile's nice
// value; a lower priority set later applies live, and the Ekin process
// keeps its own.
func TestNicerLowersJobProcesses(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	var nice atomic.Int64
	nice.Store(7)
	var changed broadcast.Signal
	n := &cpulimit.Nicer{
		Nice: func(context.Context) int { return int(nice.Load()) }, Changed: changed.Wait,
		Period: time.Hour, Log: slog.New(slog.DiscardHandler),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	waitNice(t, cmd.Process.Pid, 7)
	nice.Store(12)
	changed.Notify() // the period is an hour: only the change can wake it
	waitNice(t, cmd.Process.Pid, 12)

	var grandchild int
	deadline := time.Now().Add(3 * time.Second)
	for grandchild == 0 && time.Now().Before(deadline) {
		out, _ := exec.Command("pgrep", "-P", strconv.Itoa(cmd.Process.Pid)).Output()
		grandchild, _ = strconv.Atoi(strings.TrimSpace(string(out)))
		time.Sleep(20 * time.Millisecond)
	}
	if grandchild == 0 {
		t.Fatal("no sleep under the shell")
	}
	changed.Notify()
	waitNice(t, grandchild, 12)

	if self := niceOf(t, os.Getpid()); self != 0 {
		t.Fatalf("the Ekin process itself was reniced to %d", self)
	}
}
