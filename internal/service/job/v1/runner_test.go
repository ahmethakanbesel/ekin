package job_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	jobsvc "kurgu/internal/service/job/v1"
	"kurgu/pkg/broadcast"
	"kurgu/pkg/id"
)

type fixture struct {
	svc *jobsvc.Service
	ws  uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, time.Now())
	if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	return fixture{svc: jobsvc.NewService(jobrepo.NewSQLite(tx), tx), ws: w.ID()}
}

func (f fixture) enqueue(t *testing.T, q jobdomain.Queue, kind string) *jobdomain.Job {
	t.Helper()
	j, ok, err := f.svc.Enqueue(context.Background(), jobdomain.Spec{WorkspaceID: f.ws, Queue: q, Kind: kind, MaxAttempts: 2})
	if err != nil || !ok {
		t.Fatalf("enqueue: %v %v", ok, err)
	}
	return j
}

func (f fixture) waitFor(t *testing.T, j *jobdomain.Job, want func(*jobdomain.Job) bool) *jobdomain.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := f.svc.Get(context.Background(), f.ws, j.ID())
		if err != nil {
			t.Fatal(err)
		}
		if want(got) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _ := f.svc.Get(context.Background(), f.ws, j.ID())
	t.Fatalf("job never reached the wanted state; last status %s attempt %d", got.Status(), got.Attempt())
	return nil
}

func status(s jobdomain.Status) func(*jobdomain.Job) bool {
	return func(j *jobdomain.Job) bool { return j.Status() == s }
}

func quietLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

func startRunner(t *testing.T, f fixture, throttle jobsvc.Throttle, lease time.Duration, register func(r *jobsvc.Runner)) context.CancelFunc {
	t.Helper()
	r := jobsvc.NewRunner(f.svc, jobsvc.RunnerConfig{Owner: "test-" + id.New().String(), Throttle: throttle, Lease: lease, Poll: 5 * time.Millisecond}, quietLog())
	register(r)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return cancel
}

func one(q jobdomain.Queue) jobsvc.Throttle {
	return jobsvc.Throttle{Concurrency: map[jobdomain.Queue]int{q: 1}}
}

func TestRunnerRunsAndRecordsOutcome(t *testing.T) {
	f := newFixture(t)
	ok := f.enqueue(t, jobdomain.QueueIO, "ok")
	bad := f.enqueue(t, jobdomain.QueueIO, "bad")
	permanent := f.enqueue(t, jobdomain.QueueIO, "permanent")
	panics := f.enqueue(t, jobdomain.QueueIO, "panics")

	startRunner(t, f, one(jobdomain.QueueIO), time.Minute, func(r *jobsvc.Runner) {
		r.Register("ok", func(context.Context, *jobdomain.Job) error { return nil })
		r.Register("bad", func(context.Context, *jobdomain.Job) error { return errors.New("transient") })
		r.Register("permanent", func(context.Context, *jobdomain.Job) error {
			return jobdomain.Permanent(errors.New("bad input"))
		})
		r.Register("panics", func(context.Context, *jobdomain.Job) error { panic("boom") })
	})

	f.waitFor(t, ok, status(jobdomain.StatusSucceeded))
	retried := f.waitFor(t, bad, func(j *jobdomain.Job) bool { return j.Status() == jobdomain.StatusQueued && j.Attempt() == 1 })
	if retried.LastError() != "transient" || !retried.RunAfter().After(retried.UpdatedAt()) {
		t.Fatalf("retry not scheduled: err=%q runAfter=%v", retried.LastError(), retried.RunAfter())
	}
	f.waitFor(t, permanent, status(jobdomain.StatusFailed))
	f.waitFor(t, panics, status(jobdomain.StatusFailed))
}

func TestRunnerLeavesUnregisteredKinds(t *testing.T) {
	f := newFixture(t)
	foreign := f.enqueue(t, jobdomain.QueueIO, "someone-else")
	mine := f.enqueue(t, jobdomain.QueueIO, "mine")
	startRunner(t, f, one(jobdomain.QueueIO), time.Minute, func(r *jobsvc.Runner) {
		r.Register("mine", func(context.Context, *jobdomain.Job) error { return nil })
	})
	f.waitFor(t, mine, status(jobdomain.StatusSucceeded))
	got, _ := f.svc.Get(context.Background(), f.ws, foreign.ID())
	if got.Status() != jobdomain.StatusQueued || got.Attempt() != 0 {
		t.Fatalf("unregistered kind was touched: %s attempt %d", got.Status(), got.Attempt())
	}
}

func TestRunnerHeavyExclusivity(t *testing.T) {
	tests := []struct {
		name      string
		exclusive bool
		heavy     []jobdomain.Queue
		wantMax   int32
	}{
		{"exclusive runs one heavy job at a time", true, nil, 1},
		{"non-exclusive overlaps heavy queues", false, nil, 2},
		{"a draft runs beside a render when agent is not heavy", true, []jobdomain.Queue{jobdomain.QueueTTS, jobdomain.QueueRender}, 2},
		{"listed heavy queues stay exclusive", true, []jobdomain.Queue{jobdomain.QueueAgent, jobdomain.QueueRender}, 1},
		{"no heavy queue overlaps everything", true, []jobdomain.Queue{}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			var running, peak atomic.Int32
			gate := make(chan struct{})
			var once sync.Once
			work := func(ctx context.Context, _ *jobdomain.Job) error {
				n := running.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				if n == 2 {
					once.Do(func() { close(gate) })
				}
				select {
				case <-gate:
				case <-time.After(150 * time.Millisecond):
				case <-ctx.Done():
				}
				running.Add(-1)
				return nil
			}
			agent := f.enqueue(t, jobdomain.QueueAgent, "agent")
			render := f.enqueue(t, jobdomain.QueueRender, "render")
			throttle := jobsvc.Throttle{
				Concurrency:    map[jobdomain.Queue]int{jobdomain.QueueAgent: 1, jobdomain.QueueRender: 1},
				HeavyExclusive: tt.exclusive, Heavy: tt.heavy,
			}
			startRunner(t, f, throttle, time.Minute, func(r *jobsvc.Runner) {
				r.Register("agent", work)
				r.Register("render", work)
			})
			f.waitFor(t, agent, status(jobdomain.StatusSucceeded))
			f.waitFor(t, render, status(jobdomain.StatusSucceeded))
			if got := peak.Load(); got != tt.wantMax {
				t.Fatalf("peak concurrency = %d, want %d", got, tt.wantMax)
			}
		})
	}
}

func TestRunnerStopsCancelledJob(t *testing.T) {
	f := newFixture(t)
	j := f.enqueue(t, jobdomain.QueueIO, "slow")
	started := make(chan struct{})
	stopped := make(chan struct{})
	startRunner(t, f, one(jobdomain.QueueIO), 60*time.Millisecond, func(r *jobsvc.Runner) {
		r.Register("slow", func(ctx context.Context, _ *jobdomain.Job) error {
			close(started)
			<-ctx.Done()
			close(stopped)
			return ctx.Err()
		})
	})
	<-started
	if _, err := f.svc.Cancel(context.Background(), f.ws, j.ID()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("handler not stopped after cancel")
	}
	got := f.waitFor(t, j, status(jobdomain.StatusCancelled))
	if got.LastError() != "" {
		t.Fatalf("cancelled job got an outcome written: %q", got.LastError())
	}
}

func TestRunnerReleasesJobOnShutdown(t *testing.T) {
	f := newFixture(t)
	j := f.enqueue(t, jobdomain.QueueIO, "slow")
	started := make(chan struct{})
	cancel := startRunner(t, f, one(jobdomain.QueueIO), time.Minute, func(r *jobsvc.Runner) {
		r.Register("slow", func(ctx context.Context, _ *jobdomain.Job) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	})
	<-started
	cancel()
	got := f.waitFor(t, j, status(jobdomain.StatusQueued))
	if got.Attempt() != 0 {
		t.Fatalf("shutdown spent an attempt: %d", got.Attempt())
	}
}

func TestRunnerPausedClaimsNothing(t *testing.T) {
	f := newFixture(t)
	j := f.enqueue(t, jobdomain.QueueIO, "ok")
	throttle := one(jobdomain.QueueIO)
	throttle.Paused = true
	startRunner(t, f, throttle, time.Minute, func(r *jobsvc.Runner) {
		r.Register("ok", func(context.Context, *jobdomain.Job) error { return nil })
	})
	time.Sleep(50 * time.Millisecond)
	got, _ := f.svc.Get(context.Background(), f.ws, j.ID())
	if got.Status() != jobdomain.StatusQueued {
		t.Fatalf("paused runner ran a job: %s", got.Status())
	}
}

func TestClaimIsExclusive(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, jobdomain.QueueIO, "x")
	var wg sync.WaitGroup
	var claimed atomic.Int32
	for i := range 8 {
		wg.Go(func() {
			j, err := f.svc.Claim(context.Background(), []jobdomain.Queue{jobdomain.QueueIO}, []string{"x"}, "w"+string(rune('a'+i)), time.Minute)
			if err != nil {
				t.Error(err)
			}
			if j != nil {
				claimed.Add(1)
			}
		})
	}
	wg.Wait()
	if n := claimed.Load(); n != 1 {
		t.Fatalf("job claimed %d times", n)
	}
}

func TestRunnerFollowsThrottleSource(t *testing.T) {
	f := newFixture(t)
	j := f.enqueue(t, jobdomain.QueueIO, "ok")
	var paused atomic.Bool
	paused.Store(true)
	r := jobsvc.NewRunner(f.svc, jobsvc.RunnerConfig{
		Owner: "dyn", Poll: 5 * time.Millisecond, Lease: time.Minute,
		ThrottleSource: func(context.Context) jobsvc.Throttle {
			return jobsvc.Throttle{Concurrency: map[jobdomain.Queue]int{jobdomain.QueueIO: 1}, Paused: paused.Load()}
		},
	}, quietLog())
	r.Register("ok", func(context.Context, *jobdomain.Job) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	time.Sleep(50 * time.Millisecond)
	if got, _ := f.svc.Get(context.Background(), f.ws, j.ID()); got.Status() != jobdomain.StatusQueued {
		t.Fatalf("ran while paused: %s", got.Status())
	}
	paused.Store(false)
	f.waitFor(t, j, status(jobdomain.StatusSucceeded))
}

// Jobs enqueued while a job runs join its trace: one request, one trace.
func TestRunnerJobsJoinTheirTrace(t *testing.T) {
	f := newFixture(t)
	parent := f.enqueue(t, jobdomain.QueueIO, "parent")
	children := make(chan *jobdomain.Job, 1)
	startRunner(t, f, one(jobdomain.QueueIO), time.Minute, func(r *jobsvc.Runner) {
		r.Register("parent", func(ctx context.Context, _ *jobdomain.Job) error {
			c, _, err := f.svc.Enqueue(ctx, jobdomain.Spec{WorkspaceID: f.ws, Queue: jobdomain.QueueControl, Kind: "child"})
			children <- c
			return err
		})
	})
	f.waitFor(t, parent, status(jobdomain.StatusSucceeded))
	child := <-children
	if child.TraceID() != parent.ID() || parent.TraceID() != parent.ID() {
		t.Fatalf("child trace %s, parent %s (id %s)", child.TraceID(), parent.TraceID(), parent.ID())
	}
	got, _ := f.svc.Get(context.Background(), f.ws, parent.ID())
	if got.StartedAt().IsZero() || got.FinishedAt().Before(got.StartedAt()) {
		t.Fatalf("started %v finished %v", got.StartedAt(), got.FinishedAt())
	}
}

// A throttle change reaches slots that are waiting at once: an idle slot
// polling once an hour, and a heavy slot resting an hour-long cooldown.
func TestThrottleChangeWakesWaitingSlots(t *testing.T) {
	f := newFixture(t)
	var (
		mu       sync.Mutex
		throttle = jobsvc.Throttle{
			Concurrency:    map[jobdomain.Queue]int{jobdomain.QueueAgent: 1},
			HeavyExclusive: true, Cooldown: time.Hour, Paused: true,
		}
		changed broadcast.Signal
	)
	set := func(edit func(*jobsvc.Throttle)) {
		mu.Lock()
		edit(&throttle)
		mu.Unlock()
		changed.Notify()
	}
	r := jobsvc.NewRunner(f.svc, jobsvc.RunnerConfig{
		Owner: "dyn", Poll: time.Hour, Lease: time.Minute,
		ThrottleSource: func(context.Context) jobsvc.Throttle {
			mu.Lock()
			defer mu.Unlock()
			return throttle
		},
		ThrottleChanged: changed.Wait,
	}, quietLog())
	r.Register("agent", func(context.Context, *jobdomain.Job) error { return nil })
	first := f.enqueue(t, jobdomain.QueueAgent, "agent")
	second := f.enqueue(t, jobdomain.QueueAgent, "agent")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	time.Sleep(50 * time.Millisecond)
	set(func(t *jobsvc.Throttle) { t.Paused = false }) // the paused slot sleeps an hour unless woken
	f.waitFor(t, first, status(jobdomain.StatusSucceeded))
	time.Sleep(50 * time.Millisecond)
	if got, _ := f.svc.Get(context.Background(), f.ws, second.ID()); got.Status() != jobdomain.StatusQueued {
		t.Fatalf("second job ran during the cooldown: %s", got.Status())
	}
	set(func(t *jobsvc.Throttle) { t.Cooldown = 0 })
	f.waitFor(t, second, status(jobdomain.StatusSucceeded))
}
