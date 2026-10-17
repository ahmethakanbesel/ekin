package job_test

import (
	"cmp"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/internal/db/dbtest"
	jobdomain "ekin/internal/domain/job/v1"
	workspacedomain "ekin/internal/domain/workspace/v1"
	jobrepo "ekin/internal/repository/job/v1"
	workspacerepo "ekin/internal/repository/workspace/v1"
	"ekin/pkg/id"
)

var t0 = time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC)

const lease = 2 * time.Minute

type harness struct {
	repo      jobrepo.Repository
	workspace func(t *testing.T) uuid.UUID
}

func TestConformance(t *testing.T) {
	impls := map[string]func(t *testing.T) harness{
		"inmemory": func(*testing.T) harness {
			return harness{repo: jobrepo.NewInMemory(), workspace: func(*testing.T) uuid.UUID { return id.New() }}
		},
		"sqlite": func(t *testing.T) harness {
			_, tx := dbtest.Open(t)
			workspaces := workspacerepo.NewSQLite(tx)
			return harness{
				repo: jobrepo.NewSQLite(tx),
				workspace: func(t *testing.T) uuid.UUID {
					w, _ := workspacedomain.New(id.New(), "ws", time.UTC, t0)
					if err := workspaces.Create(context.Background(), w); err != nil {
						t.Fatal(err)
					}
					return w.ID()
				},
			}
		},
	}
	for name, newHarness := range impls {
		t.Run(name, func(t *testing.T) { runConformance(t, newHarness) })
	}
}

func newJob(t *testing.T, ws uuid.UUID, q jobdomain.Queue, kind, key string, created time.Time) *jobdomain.Job {
	t.Helper()
	j, err := jobdomain.New(id.New(), jobdomain.Spec{
		WorkspaceID: ws, Queue: q, Kind: kind, UniqueKey: key,
		Payload: json.RawMessage(`{"n":1}`), MaxAttempts: 2,
	}, created)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func runConformance(t *testing.T, newHarness func(t *testing.T) harness) {
	ctx := context.Background()

	t.Run("enqueue get round trip", func(t *testing.T) {
		h := newHarness(t)
		j := newJob(t, h.workspace(t), jobdomain.QueueRender, "render", "", t0)
		ok, err := h.repo.Enqueue(ctx, j)
		if err != nil || !ok {
			t.Fatalf("enqueue = %v, %v", ok, err)
		}
		got, err := h.repo.Get(ctx, j.WorkspaceID(), j.ID())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Snapshot(), j.Snapshot()) {
			t.Fatalf("got %+v\nwant %+v", got.Snapshot(), j.Snapshot())
		}
		if _, err := h.repo.Get(ctx, h.workspace(t), j.ID()); !apperror.Is(err, apperror.JobNotFound) {
			t.Fatalf("cross-workspace get err = %v", err)
		}
	})

	t.Run("unique key enqueues once, even after the first finished", func(t *testing.T) {
		h := newHarness(t)
		ws := h.workspace(t)
		first := newJob(t, ws, jobdomain.QueueControl, "pipeline.run", "slot-1", t0)
		if ok, _ := h.repo.Enqueue(ctx, first); !ok {
			t.Fatal("first enqueue skipped")
		}
		_ = first.Start("w", lease, t0)
		_ = first.Succeed("w", t0)
		if err := h.repo.Save(ctx, first); err != nil {
			t.Fatal(err)
		}
		dup := newJob(t, ws, jobdomain.QueueControl, "pipeline.run", "slot-1", t0)
		if ok, err := h.repo.Enqueue(ctx, dup); ok || err != nil {
			t.Fatalf("duplicate enqueue = %v, %v", ok, err)
		}
		other := newJob(t, ws, jobdomain.QueueControl, "pipeline.run", "slot-2", t0)
		if ok, _ := h.repo.Enqueue(ctx, other); !ok {
			t.Fatal("distinct key skipped")
		}
		blankA := newJob(t, ws, jobdomain.QueueControl, "x", "", t0)
		blankB := newJob(t, ws, jobdomain.QueueControl, "x", "", t0)
		for _, j := range []*jobdomain.Job{blankA, blankB} {
			if ok, _ := h.repo.Enqueue(ctx, j); !ok {
				t.Fatal("blank keys must not collide")
			}
		}
	})

	t.Run("next claimable filters by queue and kind, oldest due first", func(t *testing.T) {
		h := newHarness(t)
		ws := h.workspace(t)
		older := newJob(t, ws, jobdomain.QueueRender, "render", "", t0)
		newer := newJob(t, ws, jobdomain.QueueRender, "render", "", t0.Add(time.Second))
		otherQueue := newJob(t, ws, jobdomain.QueueIO, "render", "", t0.Add(-time.Hour))
		otherKind := newJob(t, ws, jobdomain.QueueRender, "tts", "", t0.Add(-time.Hour))
		for _, j := range []*jobdomain.Job{newer, older, otherQueue, otherKind} {
			if _, err := h.repo.Enqueue(ctx, j); err != nil {
				t.Fatal(err)
			}
		}
		got, err := h.repo.NextClaimable(ctx, []jobdomain.Queue{jobdomain.QueueRender}, []string{"render"}, t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || got.ID() != older.ID() {
			t.Fatalf("claimed %v, want the older render job", got)
		}
		if got, _ := h.repo.NextClaimable(ctx, []jobdomain.Queue{jobdomain.QueueRender}, []string{"render"}, t0.Add(-time.Minute)); got != nil {
			t.Fatal("claimed a job before it was due")
		}
		if got, _ := h.repo.NextClaimable(ctx, nil, []string{"render"}, t0.Add(time.Minute)); got != nil {
			t.Fatal("claimed with no queues")
		}
	})

	t.Run("expired lease is reclaimable until attempts run out", func(t *testing.T) {
		h := newHarness(t)
		j := newJob(t, h.workspace(t), jobdomain.QueueRender, "render", "", t0)
		_, _ = h.repo.Enqueue(ctx, j)
		_ = j.Start("a", lease, t0)
		_ = h.repo.Save(ctx, j)
		queues, kinds := []jobdomain.Queue{jobdomain.QueueRender}, []string{"render"}

		if got, _ := h.repo.NextClaimable(ctx, queues, kinds, t0.Add(lease)); got != nil {
			t.Fatal("claimed while the lease runs")
		}
		expired := t0.Add(lease + time.Second)
		got, _ := h.repo.NextClaimable(ctx, queues, kinds, expired)
		if got == nil || got.ID() != j.ID() {
			t.Fatal("expired lease not reclaimable")
		}
		_ = got.Start("b", lease, expired)
		_ = h.repo.Save(ctx, got)

		late := expired.Add(2 * lease)
		if got, _ := h.repo.NextClaimable(ctx, queues, kinds, late); got != nil {
			t.Fatal("reclaimed a job with no attempts left")
		}
		abandoned, err := h.repo.ListAbandoned(ctx, late, 10)
		if err != nil || len(abandoned) != 1 || abandoned[0].ID() != j.ID() {
			t.Fatalf("abandoned = %d, %v", len(abandoned), err)
		}
	})

	t.Run("jobs sharing a concurrency key run one at a time", func(t *testing.T) {
		queues, kinds := []jobdomain.Queue{jobdomain.QueueControl}, []string{"run"}
		tests := []struct {
			name string
			// first runs (started at t0 with lease) unless finish says it ended
			finish   func(j *jobdomain.Job)
			at       time.Time
			otherKey string // the second job's key
			want     bool   // the second job is claimable at at
		}{
			{name: "held while the first runs", at: t0.Add(time.Second), otherKey: "pipeline:a"},
			{name: "free once the first finished", finish: func(j *jobdomain.Job) { _ = j.Succeed("a", t0.Add(time.Second)) }, at: t0.Add(2 * time.Second), otherKey: "pipeline:a", want: true},
			{name: "free once the first's lease ran out", at: t0.Add(lease + time.Second), otherKey: "pipeline:a", want: true},
			{name: "another key runs alongside", at: t0.Add(time.Second), otherKey: "pipeline:b", want: true},
			{name: "no key runs alongside", at: t0.Add(time.Second), otherKey: "", want: true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness(t)
				ws := h.workspace(t)
				first, _ := jobdomain.New(id.New(), jobdomain.Spec{WorkspaceID: ws, Queue: jobdomain.QueueControl, Kind: "run", ConcurrencyKey: "pipeline:a"}, t0)
				second, _ := jobdomain.New(id.New(), jobdomain.Spec{WorkspaceID: ws, Queue: jobdomain.QueueControl, Kind: "run", ConcurrencyKey: tt.otherKey}, t0.Add(time.Millisecond))
				_, _ = h.repo.Enqueue(ctx, first)
				_, _ = h.repo.Enqueue(ctx, second)
				_ = first.Start("a", lease, t0)
				if tt.finish != nil {
					tt.finish(first)
				}
				_ = h.repo.Save(ctx, first)
				got, err := h.repo.NextClaimable(ctx, queues, kinds, tt.at)
				if err != nil {
					t.Fatal(err)
				}
				// An expired first job is itself claimable again, and older.
				claimable := got != nil && (got.ID() == second.ID() || got.ID() == first.ID() && tt.want)
				if claimable != tt.want {
					t.Fatalf("claimable = %v (got %v), want %v", claimable, got, tt.want)
				}
				if got != nil && got.ID() == second.ID() && got.ConcurrencyKey() != tt.otherKey {
					t.Fatalf("key = %q", got.ConcurrencyKey())
				}
			})
		}
	})

	t.Run("save persists transitions", func(t *testing.T) {
		h := newHarness(t)
		j := newJob(t, h.workspace(t), jobdomain.QueueAgent, "agent", "", t0)
		_, _ = h.repo.Enqueue(ctx, j)
		_ = j.Start("w", lease, t0)
		_ = j.Fail("w", context.DeadlineExceeded, t0.Add(time.Second))
		if err := h.repo.Save(ctx, j); err != nil {
			t.Fatal(err)
		}
		got, _ := h.repo.GetByID(ctx, j.ID())
		if !reflect.DeepEqual(got.Snapshot(), j.Snapshot()) {
			t.Fatalf("got %+v\nwant %+v", got.Snapshot(), j.Snapshot())
		}
		ghost := newJob(t, j.WorkspaceID(), jobdomain.QueueAgent, "agent", "", t0)
		if err := h.repo.Save(ctx, ghost); !apperror.Is(err, apperror.JobNotFound) {
			t.Fatalf("save missing err = %v", err)
		}
	})

	t.Run("list newest first with status filter", func(t *testing.T) {
		h := newHarness(t)
		ws := h.workspace(t)
		a := newJob(t, ws, jobdomain.QueueIO, "x", "", t0)
		b := newJob(t, ws, jobdomain.QueueIO, "x", "", t0.Add(time.Minute))
		c := newJob(t, ws, jobdomain.QueueIO, "x", "", t0.Add(2*time.Minute))
		foreign := newJob(t, h.workspace(t), jobdomain.QueueIO, "x", "", t0)
		for _, j := range []*jobdomain.Job{a, b, c, foreign} {
			_, _ = h.repo.Enqueue(ctx, j)
		}
		_ = b.Cancel(t0.Add(3 * time.Minute))
		_ = h.repo.Save(ctx, b)

		all, _, _ := h.repo.List(ctx, ws, jobrepo.Filter{})
		if len(all) != 3 || all[0].ID() != c.ID() || all[2].ID() != a.ID() {
			t.Fatalf("list = %d items in wrong order", len(all))
		}
		queued, _, _ := h.repo.List(ctx, ws, jobrepo.Filter{Statuses: []jobdomain.Status{jobdomain.StatusQueued}})
		if len(queued) != 2 {
			t.Fatalf("queued = %d, want 2", len(queued))
		}
		limited, _, _ := h.repo.List(ctx, ws, jobrepo.Filter{Limit: 1})
		if len(limited) != 1 || limited[0].ID() != c.ID() {
			t.Fatal("limit ignored")
		}
	})

	t.Run("list whole requests", func(t *testing.T) {
		h := newHarness(t)
		ws := h.workspace(t)
		a := newJob(t, ws, jobdomain.QueueControl, "plan", "", t0)
		b := newJob(t, ws, jobdomain.QueueIO, "other", "", t0.Add(time.Minute))
		step, err := jobdomain.New(id.New(), jobdomain.Spec{WorkspaceID: ws, Queue: jobdomain.QueueAgent, Kind: "draft", TraceID: a.ID()}, t0.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		for _, j := range []*jobdomain.Job{a, b, step} {
			_, _ = h.repo.Enqueue(ctx, j)
		}
		_ = step.Start("w", time.Minute, t0.Add(3*time.Minute))
		_ = h.repo.Save(ctx, step)

		// a's request is the newest (its step came last) and comes whole.
		latest, _, _ := h.repo.List(ctx, ws, jobrepo.Filter{Limit: 1})
		if len(latest) != 2 || latest[0].ID() != step.ID() || latest[1].ID() != a.ID() {
			t.Fatalf("latest request = %d jobs", len(latest))
		}
		if !latest[0].StartedAt().Equal(t0.Add(3*time.Minute)) || latest[0].TraceID() != a.ID() {
			t.Fatalf("step = %+v", latest[0].Snapshot())
		}
		// A status keeps the requests with a job in it, with all their jobs.
		running, _, _ := h.repo.List(ctx, ws, jobrepo.Filter{Statuses: []jobdomain.Status{jobdomain.StatusRunning}})
		if len(running) != 2 {
			t.Fatalf("running requests = %d jobs, want 2", len(running))
		}
		if all, _, _ := h.repo.List(ctx, ws, jobrepo.Filter{}); len(all) != 3 {
			t.Fatalf("all = %d", len(all))
		}
		// A kind keeps the requests with a job of it, with all their jobs.
		drafts, _, _ := h.repo.List(ctx, ws, jobrepo.Filter{Kinds: []string{"draft"}})
		if len(drafts) != 2 || drafts[1].ID() != a.ID() {
			t.Fatalf("draft requests = %d jobs, want a's 2", len(drafts))
		}
	})

	t.Run("pages through requests once each", func(t *testing.T) {
		for _, size := range []int{1, 2, 3, 6} {
			h := newHarness(t)
			ws := h.workspace(t)
			type request struct {
				at time.Time
				id string
			}
			var want []request
			for i := range 5 {
				// Two requests share each instant, so ties go to the trace.
				at := t0.Add(time.Duration(i/2) * time.Minute)
				j := newJob(t, ws, jobdomain.QueueIO, "x", "", at)
				_, _ = h.repo.Enqueue(ctx, j)
				want = append(want, request{at, j.ID().String()})
			}
			slices.SortFunc(want, func(a, b request) int {
				return cmp.Or(b.at.Compare(a.at), strings.Compare(b.id, a.id))
			})
			var got []string
			f := jobrepo.Filter{Limit: size}
			for range 10 {
				list, next, err := h.repo.List(ctx, ws, f)
				if err != nil {
					t.Fatal(err)
				}
				for _, j := range list {
					got = append(got, j.ID().String())
				}
				if next == "" {
					break
				}
				f.After = next
			}
			if len(got) != len(want) {
				t.Fatalf("size %d: got %d requests, want %d", size, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i].id {
					t.Fatalf("size %d: request %d = %s, want %s", size, i, got[i], want[i].id)
				}
			}
		}
		h := newHarness(t)
		if _, _, err := h.repo.List(ctx, h.workspace(t), jobrepo.Filter{After: "nope"}); !apperror.Is(err, apperror.ValidationFailed) {
			t.Fatalf("bad cursor = %v", err)
		}
	})
}
