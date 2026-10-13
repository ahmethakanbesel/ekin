package pipeline_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	pipelinerepo "kurgu/internal/repository/pipeline/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
)

var t0 = time.Date(2026, 10, 13, 7, 0, 0, 0, time.UTC)

type harness struct {
	repo pipelinerepo.Repository
	// workspace returns a workspace id the repository will accept.
	workspace func(t *testing.T) uuid.UUID
}

func TestConformance(t *testing.T) {
	impls := map[string]func(t *testing.T) harness{
		"inmemory": func(*testing.T) harness {
			return harness{repo: pipelinerepo.NewInMemory(), workspace: func(*testing.T) uuid.UUID { return id.New() }}
		},
		"sqlite": func(t *testing.T) harness {
			_, tx := dbtest.Open(t)
			workspaces := workspacerepo.NewSQLite(tx)
			return harness{
				repo: pipelinerepo.NewSQLite(tx),
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

func newPipeline(t *testing.T, workspaceID uuid.UUID, cron string, now time.Time) *pipelinedomain.Pipeline {
	t.Helper()
	ist, _ := time.LoadLocation("Europe/Istanbul")
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 45*time.Second)
	article, _ := pipelinedomain.NewArticleSpec(900, []pipelinedomain.Language{pipelinedomain.English})
	outputs, _ := pipelinedomain.NewOutputs(&video, &article, pipelinedomain.KindVideo)
	pages, _ := pipelinedomain.NewPageRange(4, 12)
	ref, _ := pipelinedomain.NewKnowledgeRef(id.New(), pages)
	source, _ := pipelinedomain.NewKnowledgeSource("Yalnızca bu bölümden", []pipelinedomain.KnowledgeRef{ref})
	def := pipelinedomain.Definition{Name: "Kitap shorts", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source}
	if cron != "" {
		s, err := pipelinedomain.ParseSchedule(cron, ist, 3)
		if err != nil {
			t.Fatal(err)
		}
		def.Schedule = &s
	}
	p, err := pipelinedomain.New(id.New(), workspaceID, def, now)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func runConformance(t *testing.T, newHarness func(t *testing.T) harness) {
	ctx := context.Background()

	t.Run("create get round trip", func(t *testing.T) {
		h := newHarness(t)
		p := newPipeline(t, h.workspace(t), "0 2 * * *", t0)
		if err := h.repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		got, err := h.repo.Get(ctx, p.WorkspaceID(), p.ID())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Snapshot(), p.Snapshot()) {
			t.Fatalf("got  %+v\nwant %+v", got.Snapshot(), p.Snapshot())
		}
	})

	t.Run("manual pipeline round trip", func(t *testing.T) {
		h := newHarness(t)
		p := newPipeline(t, h.workspace(t), "", t0)
		if err := h.repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		got, err := h.repo.Get(ctx, p.WorkspaceID(), p.ID())
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.Schedule(); ok {
			t.Fatal("manual pipeline came back with a schedule")
		}
	})

	t.Run("get is workspace scoped", func(t *testing.T) {
		h := newHarness(t)
		p := newPipeline(t, h.workspace(t), "", t0)
		if err := h.repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		other := h.workspace(t)
		if _, err := h.repo.Get(ctx, other, p.ID()); !apperror.Is(err, apperror.PipelineNotFound) {
			t.Fatalf("cross-workspace get err = %v", err)
		}
		if err := h.repo.Delete(ctx, other, p.ID()); !apperror.Is(err, apperror.PipelineNotFound) {
			t.Fatalf("cross-workspace delete err = %v", err)
		}
	})

	t.Run("update bumps version and rejects stale writers", func(t *testing.T) {
		h := newHarness(t)
		p := newPipeline(t, h.workspace(t), "0 2 * * *", t0)
		if err := h.repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		a, _ := h.repo.Get(ctx, p.WorkspaceID(), p.ID())
		b, _ := h.repo.Get(ctx, p.WorkspaceID(), p.ID())

		if err := a.Pause(t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		saved, err := h.repo.Update(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Version() != p.Version()+1 {
			t.Fatalf("version = %d, want %d", saved.Version(), p.Version()+1)
		}
		_, _ = b.RunNow(1, t0.Add(time.Minute))
		if _, err := h.repo.Update(ctx, b); !apperror.Is(err, apperror.StaleVersion) {
			t.Fatalf("stale update err = %v, want StaleVersion", err)
		}
		got, _ := h.repo.Get(ctx, p.WorkspaceID(), p.ID())
		if got.Status() != pipelinedomain.StatusPaused || !reflect.DeepEqual(got.Snapshot(), saved.Snapshot()) {
			t.Fatalf("stored %+v, want %+v", got.Snapshot(), saved.Snapshot())
		}
	})

	t.Run("update missing", func(t *testing.T) {
		h := newHarness(t)
		p := newPipeline(t, h.workspace(t), "", t0)
		if _, err := h.repo.Update(ctx, p); !apperror.Is(err, apperror.PipelineNotFound) {
			t.Fatalf("err = %v, want PipelineNotFound", err)
		}
	})

	t.Run("list oldest first", func(t *testing.T) {
		h := newHarness(t)
		ws := h.workspace(t)
		second := newPipeline(t, ws, "", t0.Add(time.Hour))
		first := newPipeline(t, ws, "", t0)
		foreign := newPipeline(t, h.workspace(t), "", t0)
		for _, p := range []*pipelinedomain.Pipeline{second, first, foreign} {
			if err := h.repo.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
		}
		got, err := h.repo.List(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].ID() != first.ID() || got[1].ID() != second.ID() {
			t.Fatalf("list = %d items, want first then second", len(got))
		}
	})

	t.Run("delete", func(t *testing.T) {
		h := newHarness(t)
		p := newPipeline(t, h.workspace(t), "", t0)
		_ = h.repo.Create(ctx, p)
		if err := h.repo.Delete(ctx, p.WorkspaceID(), p.ID()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.repo.Get(ctx, p.WorkspaceID(), p.ID()); !apperror.Is(err, apperror.PipelineNotFound) {
			t.Fatalf("err after delete = %v", err)
		}
	})

	t.Run("list due across workspaces, earliest first, skipping paused and manual", func(t *testing.T) {
		h := newHarness(t)
		// Nightly at 02:00 Istanbul fires 23:00 UTC; 06:00 fires 03:00 UTC next day.
		early := newPipeline(t, h.workspace(t), "0 2 * * *", t0)
		late := newPipeline(t, h.workspace(t), "0 6 * * *", t0)
		paused := newPipeline(t, h.workspace(t), "0 1 * * *", t0)
		_ = paused.Pause(t0)
		manual := newPipeline(t, h.workspace(t), "", t0)
		for _, p := range []*pipelinedomain.Pipeline{late, early, paused, manual} {
			if err := h.repo.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
		}
		at := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
		got, err := h.repo.ListDue(ctx, at, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].ID() != early.ID() || got[1].ID() != late.ID() {
			t.Fatalf("due = %d items, want early then late", len(got))
		}
		if got, _ := h.repo.ListDue(ctx, at, 1); len(got) != 1 {
			t.Fatalf("limit ignored: %d items", len(got))
		}
		if got, _ := h.repo.ListDue(ctx, at.Add(-5*time.Hour), 10); len(got) != 0 {
			t.Fatalf("nothing should be due yet, got %d", len(got))
		}
	})
}
