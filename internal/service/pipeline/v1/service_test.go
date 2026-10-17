package pipeline_test

import (
	"context"
	"reflect"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	pipelinerepo "kurgu/internal/repository/pipeline/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	jobsvc "kurgu/internal/service/job/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	"kurgu/pkg/id"
)

// t0 is 2026-10-13 10:00 Istanbul; the nightly slot is 23:00 UTC.
var (
	t0       = time.Date(2026, 10, 13, 7, 0, 0, 0, time.UTC)
	firstRun = time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC)
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time   { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Set(t time.Time)  { c.mu.Lock(); defer c.mu.Unlock(); c.now = t }
func newClock(t time.Time) *clock { return &clock{now: t} }

type fixture struct {
	svc   *pipelinesvc.Service
	jobs  *jobsvc.Service
	clock *clock
	ws    uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	_, tx := dbtest.Open(t)
	c := newClock(t0)
	workspaces := workspacerepo.NewSQLite(tx)
	w, _ := workspacedomain.New(id.New(), "acme", time.UTC, t0)
	if err := workspaces.Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx).WithClock(c.Now)
	svc := pipelinesvc.NewService(pipelinerepo.NewSQLite(tx), workspaces, jobs, tx).WithClock(c.Now)
	return fixture{svc: svc, jobs: jobs, clock: c, ws: w.ID()}
}

func definition(t *testing.T, cron string) pipelinedomain.Definition {
	t.Helper()
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 45*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine kısa videolar")
	def := pipelinedomain.Definition{Name: "Shorts", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source}
	if cron != "" {
		ist, _ := time.LoadLocation("Europe/Istanbul")
		s, err := pipelinedomain.ParseSchedule(cron, ist, 5)
		if err != nil {
			t.Fatal(err)
		}
		def.Schedule = &s
	}
	return def
}

func (f fixture) runJobs(t *testing.T) []*jobdomain.Job {
	t.Helper()
	jobs, _, err := f.jobs.List(context.Background(), f.ws, jobrepo.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return jobs
}

func TestCreateRequiresWorkspace(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Create(context.Background(), id.New(), definition(t, ""))
	if !apperror.Is(err, apperror.WorkspaceNotFound) {
		t.Fatalf("err = %v, want WorkspaceNotFound", err)
	}
}

func TestTriggerDueEnqueuesOneRunPerSlot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.svc.Create(ctx, f.ws, definition(t, "0 2 * * *"))
	if err != nil {
		t.Fatal(err)
	}
	manual, _ := f.svc.Create(ctx, f.ws, definition(t, ""))

	f.clock.Set(firstRun.Add(-time.Minute))
	if n, err := f.svc.TriggerDue(ctx); err != nil || n != 0 {
		t.Fatalf("before slot: n=%d err=%v", n, err)
	}

	f.clock.Set(firstRun.Add(time.Second))
	if n, err := f.svc.TriggerDue(ctx); err != nil || n != 1 {
		t.Fatalf("at slot: n=%d err=%v", n, err)
	}
	if n, _ := f.svc.TriggerDue(ctx); n != 0 {
		t.Fatalf("second tick re-triggered: %d", n)
	}

	jobs := f.runJobs(t)
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	var payload pipelinesvc.RunPayload
	if err := json.Unmarshal(jobs[0].Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PipelineID != p.ID() || payload.Count != 5 || !payload.Slot.Equal(firstRun) || payload.Trigger != "schedule" {
		t.Fatalf("payload = %+v", payload)
	}
	if jobs[0].Queue() != jobdomain.QueueControl || jobs[0].Kind() != pipelinesvc.KindRun {
		t.Fatalf("job queue/kind = %s/%s", jobs[0].Queue(), jobs[0].Kind())
	}

	got, _ := f.svc.Get(ctx, f.ws, p.ID())
	if !got.NextRunAt().Equal(firstRun.Add(24 * time.Hour)) {
		t.Fatalf("next run = %v", got.NextRunAt())
	}
	if m, _ := f.svc.Get(ctx, f.ws, manual.ID()); !m.LastRunAt().IsZero() {
		t.Fatal("manual pipeline was triggered")
	}
}

func TestTriggerDueAfterDowntimeRunsOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Create(ctx, f.ws, definition(t, "0 2 * * *")); err != nil {
		t.Fatal(err)
	}
	f.clock.Set(firstRun.Add(3 * 24 * time.Hour))
	if n, err := f.svc.TriggerDue(ctx); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, _ := f.svc.TriggerDue(ctx); n != 0 {
		t.Fatalf("backlog replayed: %d", n)
	}
}

func TestPausedPipelineIsNotTriggered(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.svc.Create(ctx, f.ws, definition(t, "0 2 * * *"))
	if _, err := f.svc.Pause(ctx, f.ws, p.ID()); err != nil {
		t.Fatal(err)
	}
	f.clock.Set(firstRun.Add(time.Hour))
	if n, _ := f.svc.TriggerDue(ctx); n != 0 {
		t.Fatalf("paused pipeline triggered: %d", n)
	}
}

func TestUpcomingMergesPipelinesSoonestFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	nightly, _ := f.svc.Create(ctx, f.ws, definition(t, "0 2 * * *"))
	morning, _ := f.svc.Create(ctx, f.ws, definition(t, "30 9 * * 1")) // Monday 06:30 UTC
	paused, _ := f.svc.Create(ctx, f.ws, definition(t, "0 12 * * *"))
	if _, err := f.svc.Pause(ctx, f.ws, paused.ID()); err != nil {
		t.Fatal(err)
	}
	_, _ = f.svc.Create(ctx, f.ws, definition(t, "")) // manual only

	runs, until, err := f.svc.Upcoming(ctx, f.ws, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(72 * time.Hour); !until.Equal(want) {
		t.Fatalf("until = %v, want %v", until, want)
	}
	night := func(day int) time.Time { return time.Date(2026, 10, day, 23, 0, 0, 0, time.UTC) }
	want := []pipelinesvc.UpcomingRun{
		{PipelineID: nightly.ID(), At: night(13), Count: 5},
		{PipelineID: nightly.ID(), At: night(14), Count: 5},
		{PipelineID: nightly.ID(), At: night(15), Count: 5},
	}
	if !reflect.DeepEqual(runs, want) {
		t.Fatalf("runs = %+v, want %+v", runs, want)
	}

	runs, _, _ = f.svc.Upcoming(ctx, f.ws, 7)
	var mondays int
	for i, r := range runs {
		if i > 0 && r.At.Before(runs[i-1].At) {
			t.Fatalf("runs out of order at %d: %v", i, runs)
		}
		if r.PipelineID == paused.ID() {
			t.Fatal("paused pipeline listed")
		}
		if r.PipelineID == morning.ID() {
			mondays++
			if want := time.Date(2026, 10, 19, 6, 30, 0, 0, time.UTC); !r.At.Equal(want) {
				t.Fatalf("monday run at %v, want %v", r.At, want)
			}
		}
	}
	if mondays != 1 || len(runs) != 8 {
		t.Fatalf("got %d runs with %d Monday runs, want 8 and 1", len(runs), mondays)
	}

	for _, days := range []int{0, pipelinesvc.MaxUpcomingDays + 1} {
		if _, _, err := f.svc.Upcoming(ctx, f.ws, days); !apperror.Is(err, apperror.ValidationFailed) {
			t.Fatalf("days %d err = %v", days, err)
		}
	}
}

func TestRunNowEnqueuesManualRun(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.svc.Create(ctx, f.ws, definition(t, ""))
	for range 2 {
		if _, err := f.svc.RunNow(ctx, f.ws, p.ID(), 3); err != nil {
			t.Fatal(err)
		}
	}
	if jobs := f.runJobs(t); len(jobs) != 2 {
		t.Fatalf("manual runs must not be deduplicated: %d jobs", len(jobs))
	}
	if _, err := f.svc.RunNow(ctx, f.ws, p.ID(), 0); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("zero count err = %v", err)
	}
	if _, err := f.svc.RunNow(ctx, id.New(), p.ID(), 1); !apperror.Is(err, apperror.PipelineNotFound) {
		t.Fatalf("foreign workspace err = %v", err)
	}
}

func TestRedefineRejectsStaleVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.svc.Create(ctx, f.ws, definition(t, ""))
	read := p.Version()

	renamed := definition(t, "")
	renamed.Name = "Renamed"
	updated, err := f.svc.Redefine(ctx, f.ws, p.ID(), read, renamed)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name() != "Renamed" || updated.Version() != read+1 {
		t.Fatalf("updated = %s v%d", updated.Name(), updated.Version())
	}
	if _, err := f.svc.Redefine(ctx, f.ws, p.ID(), read, definition(t, "")); !apperror.Is(err, apperror.StaleVersion) {
		t.Fatalf("stale redefine err = %v", err)
	}
}
