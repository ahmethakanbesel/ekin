package job_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
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

		all, _ := h.repo.List(ctx, ws, jobrepo.Filter{})
		if len(all) != 3 || all[0].ID() != c.ID() || all[2].ID() != a.ID() {
			t.Fatalf("list = %d items in wrong order", len(all))
		}
		queued, _ := h.repo.List(ctx, ws, jobrepo.Filter{Statuses: []jobdomain.Status{jobdomain.StatusQueued}})
		if len(queued) != 2 {
			t.Fatalf("queued = %d, want 2", len(queued))
		}
		limited, _ := h.repo.List(ctx, ws, jobrepo.Filter{Limit: 1})
		if len(limited) != 1 || limited[0].ID() != c.ID() {
			t.Fatal("limit ignored")
		}
	})
}
