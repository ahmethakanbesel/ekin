package analytics_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db/dbtest"
	analyticsdomain "github.com/ahmethakanbesel/ekin/internal/domain/analytics/v1"
	workspacedomain "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	analyticsrepo "github.com/ahmethakanbesel/ekin/internal/repository/analytics/v1"
	workspacerepo "github.com/ahmethakanbesel/ekin/internal/repository/workspace/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

var now = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

func impls() map[string]func(t *testing.T) (analyticsrepo.Repository, uuid.UUID, uuid.UUID) {
	return map[string]func(t *testing.T) (analyticsrepo.Repository, uuid.UUID, uuid.UUID){
		"inmemory": func(*testing.T) (analyticsrepo.Repository, uuid.UUID, uuid.UUID) {
			return analyticsrepo.NewInMemory(), id.New(), id.New()
		},
		"sqlite": func(t *testing.T) (analyticsrepo.Repository, uuid.UUID, uuid.UUID) {
			_, tx := dbtest.Open(t)
			var ids []uuid.UUID
			for _, name := range []string{"a", "b"} {
				w, _ := workspacedomain.New(id.New(), name, time.UTC, now)
				if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, w.ID())
			}
			return analyticsrepo.NewSQLite(tx), ids[0], ids[1]
		},
	}
}

func day(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

func TestConnections(t *testing.T) {
	for name, open := range impls() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws, other := open(t)
			if _, err := repo.Connection(ctx, ws); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("none = %v", err)
			}
			c, _ := analyticsdomain.Connect(ws, "sealed", id.New(), now)
			bare, _ := analyticsdomain.Connect(other, "sealed2", id.New(), now)
			_, _ = c.Choose("sc-domain:example.com", now)
			c.Synced(day(30), now)
			for _, x := range []*analyticsdomain.Connection{c, bare} {
				if err := repo.PutConnection(ctx, x); err != nil {
					t.Fatal(err)
				}
			}
			got, err := repo.Connection(ctx, ws)
			if err != nil || got.Property() != "sc-domain:example.com" || !got.SyncedThrough().Equal(day(30)) || got.SealedToken() != "sealed" {
				t.Fatalf("connection = %+v %v", got, err)
			}
			if all, _ := repo.Connections(ctx); len(all) != 1 || all[0].WorkspaceID() != ws {
				t.Fatalf("connections with a property = %d", len(all))
			}
			if err := repo.DeleteConnection(ctx, ws); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Connection(ctx, ws); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("after delete = %v", err)
			}
		})
	}
}

func TestMetrics(t *testing.T) {
	for name, open := range impls() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws, other := open(t)
			m := func(d int, key string, clicks, imps int, pos float64) analyticsdomain.Metric {
				return analyticsdomain.Metric{Date: day(d), Key: key, Clicks: clicks, Impressions: imps, Position: pos}
			}
			first := []analyticsdomain.Metric{m(1, "/a", 5, 50, 4), m(1, "/b", 1, 10, 9), m(2, "/a", 3, 30, 6), m(3, "/a", 9, 90, 2)}
			if err := repo.ReplaceMetrics(ctx, ws, analyticsdomain.DimPage, day(1), day(3), first); err != nil {
				t.Fatal(err)
			}
			_ = repo.ReplaceMetrics(ctx, other, analyticsdomain.DimPage, day(1), day(3), first[:1])
			_ = repo.ReplaceMetrics(ctx, ws, analyticsdomain.DimQuery, day(1), day(1), []analyticsdomain.Metric{m(1, "acme", 2, 20, 3)})
			// A later sync replaces day 3 and adds day 4.
			if err := repo.ReplaceMetrics(ctx, ws, analyticsdomain.DimPage, day(3), day(4), []analyticsdomain.Metric{m(3, "/a", 7, 70, 3), m(4, "/b", 4, 40, 5)}); err != nil {
				t.Fatal(err)
			}
			all, err := repo.Metrics(ctx, ws, analyticsdomain.DimPage, nil, day(1), day(4))
			if err != nil || len(all) != 5 || all[3].Clicks != 7 {
				t.Fatalf("metrics = %+v %v", all, err)
			}
			a, _ := repo.Metrics(ctx, ws, analyticsdomain.DimPage, []string{"/a"}, day(2), day(3))
			if len(a) != 2 || !a[0].Date.Equal(day(2)) {
				t.Fatalf("/a on days 2-3 = %+v", a)
			}
			top, err := repo.Top(ctx, ws, analyticsdomain.DimPage, day(1), day(4), 1)
			if err != nil || len(top) != 1 || top[0].Key != "/a" || top[0].Clicks != 15 || top[0].Days != 4 {
				t.Fatalf("top = %+v %v", top, err)
			}
			if want := (5*4.0*10 + 3*6*10 + 7*3*10) / 150; top[0].Position < want-1e-9 || top[0].Position > want+1e-9 {
				t.Fatalf("position = %v, want %v", top[0].Position, want)
			}
			if q, _ := repo.Metrics(ctx, ws, analyticsdomain.DimQuery, nil, day(1), day(4)); len(q) != 1 {
				t.Fatalf("queries = %+v", q)
			}
			if err := repo.ClearMetrics(ctx, ws); err != nil {
				t.Fatal(err)
			}
			if left, _ := repo.Metrics(ctx, ws, analyticsdomain.DimPage, nil, day(1), day(4)); len(left) != 0 {
				t.Fatal("cleared metrics remain")
			}
			if kept, _ := repo.Metrics(ctx, other, analyticsdomain.DimPage, nil, day(1), day(4)); len(kept) != 1 {
				t.Fatal("another workspace's metrics were cleared")
			}
		})
	}
}
