package cpulimit

import (
	"math"
	"reflect"
	"testing"
)

// A process that would use two CPUs, limited to 25% of a 2-CPU host,
// averages half a CPU.
func TestBucketConvergesToTheShare(t *testing.T) {
	const (
		period = 0.1 // seconds
		share  = 0.25 * 2
		demand = 2.0 // CPUs the process uses while it runs
	)
	var b bucket
	paused, total := false, 0.0
	steps := 1000
	for range steps {
		used := 0.0
		if !paused {
			used = demand * period
		}
		total += used
		paused = b.step(share*period, used, share*2*period)
	}
	if got := total / (float64(steps) * period); math.Abs(got-share) > 0.05 {
		t.Fatalf("average use = %.3f CPUs, want %.2f", got, share)
	}
}

// A process using less than its share is never paused.
func TestBucketLeavesLightUseAlone(t *testing.T) {
	var b bucket
	for range 100 {
		if b.step(0.05, 0.03, 0.1) {
			t.Fatal("paused a process under its share")
		}
	}
}

func TestParseStat(t *testing.T) {
	tests := []struct {
		line string
		want procStat
		ok   bool
	}{
		{"1234 (ffmpeg) S 1200 1234 1 0 -1 4194560 1 0 0 0 250 50 0 0 20 0 1 0 100 0 0", procStat{1234, 1200, 300}, true},
		// A name with spaces and parentheses.
		{"77 (Web Content (x)) R 1 77 77 0 -1 0 0 0 0 0 7 3 0 0 20 0 1 0 1 0 0", procStat{77, 1, 10}, true},
		{"garbage", procStat{}, false},
		{"12 (short) S 1", procStat{}, false},
	}
	for _, tt := range tests {
		got, ok := parseStat(tt.line)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseStat(%q) = %+v, %v; want %+v, %v", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDescendants(t *testing.T) {
	all := []procStat{
		{pid: 1, ppid: 0}, {pid: 10, ppid: 1}, // the server, under init
		{pid: 11, ppid: 10}, {pid: 12, ppid: 11}, {pid: 13, ppid: 12}, // claude → bash → kurgu tool
		{pid: 20, ppid: 1}, // someone else's
	}
	var got []int
	for _, p := range descendants(all, 10) {
		got = append(got, p.pid)
	}
	if !reflect.DeepEqual(got, []int{11, 12, 13}) {
		t.Fatalf("descendants = %v", got)
	}
}
