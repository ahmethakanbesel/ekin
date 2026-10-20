package workspace_test

import (
	"context"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db/dbtest"
	workspacedomain "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	workspacerepo "github.com/ahmethakanbesel/ekin/internal/repository/workspace/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

func TestConformance(t *testing.T) {
	impls := map[string]func(t *testing.T) workspacerepo.Repository{
		"inmemory": func(*testing.T) workspacerepo.Repository { return workspacerepo.NewInMemory() },
		"sqlite": func(t *testing.T) workspacerepo.Repository {
			_, tx := dbtest.Open(t)
			return workspacerepo.NewSQLite(tx)
		},
	}
	for name, newRepo := range impls {
		t.Run(name, func(t *testing.T) { runConformance(t, newRepo) })
	}
}

func runConformance(t *testing.T, newRepo func(t *testing.T) workspacerepo.Repository) {
	ctx := context.Background()
	istanbul, _ := workspacedomain.ParseTimezone("Europe/Istanbul")
	now := time.Date(2026, 10, 13, 9, 0, 0, 123_456_789, time.UTC)

	t.Run("create get round trip", func(t *testing.T) {
		repo := newRepo(t)
		w, _ := workspacedomain.New(id.New(), "acme", istanbul, now)
		if err := repo.Create(ctx, w); err != nil {
			t.Fatal(err)
		}
		got, err := repo.Get(ctx, w.ID())
		if err != nil {
			t.Fatal(err)
		}
		if got.Snapshot() != w.Snapshot() {
			t.Fatalf("got %+v, want %+v", got.Snapshot(), w.Snapshot())
		}
	})

	t.Run("get missing", func(t *testing.T) {
		repo := newRepo(t)
		_, err := repo.Get(ctx, id.New())
		if !apperror.Is(err, apperror.WorkspaceNotFound) {
			t.Fatalf("err = %v, want WorkspaceNotFound", err)
		}
	})

	t.Run("update persists changes", func(t *testing.T) {
		repo := newRepo(t)
		w, _ := workspacedomain.New(id.New(), "acme", istanbul, now)
		if err := repo.Create(ctx, w); err != nil {
			t.Fatal(err)
		}
		utc, _ := workspacedomain.ParseTimezone("UTC")
		_ = w.ChangeTimezone(utc, now.Add(time.Minute))
		_ = w.Rename("acme tr", now.Add(time.Minute))
		if err := repo.Update(ctx, w); err != nil {
			t.Fatal(err)
		}
		got, _ := repo.Get(ctx, w.ID())
		if got.Snapshot() != w.Snapshot() {
			t.Fatalf("got %+v, want %+v", got.Snapshot(), w.Snapshot())
		}
	})

	t.Run("update missing", func(t *testing.T) {
		repo := newRepo(t)
		w, _ := workspacedomain.New(id.New(), "ghost", istanbul, now)
		if err := repo.Update(ctx, w); !apperror.Is(err, apperror.WorkspaceNotFound) {
			t.Fatalf("err = %v, want WorkspaceNotFound", err)
		}
	})

	t.Run("list oldest first", func(t *testing.T) {
		repo := newRepo(t)
		second, _ := workspacedomain.New(id.New(), "second", istanbul, now.Add(time.Hour))
		first, _ := workspacedomain.New(id.New(), "first", istanbul, now)
		for _, w := range []*workspacedomain.Workspace{second, first} {
			if err := repo.Create(ctx, w); err != nil {
				t.Fatal(err)
			}
		}
		got, err := repo.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Name() != "first" || got[1].Name() != "second" {
			t.Fatalf("list order wrong: %v", names(got))
		}
	})
}

func names(ws []*workspacedomain.Workspace) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.Name())
	}
	return out
}
