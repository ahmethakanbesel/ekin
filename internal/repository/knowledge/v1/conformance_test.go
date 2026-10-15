package knowledge_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	knowledgedomain "kurgu/internal/domain/knowledge/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	knowledgerepo "kurgu/internal/repository/knowledge/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
)

var now = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

func TestConformance(t *testing.T) {
	impls := map[string]func(t *testing.T) (knowledgerepo.Repository, uuid.UUID){
		"inmemory": func(*testing.T) (knowledgerepo.Repository, uuid.UUID) { return knowledgerepo.NewInMemory(), id.New() },
		"sqlite": func(t *testing.T) (knowledgerepo.Repository, uuid.UUID) {
			_, tx := dbtest.Open(t)
			w, _ := workspacedomain.New(id.New(), "ws", time.UTC, now)
			if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
				t.Fatal(err)
			}
			return knowledgerepo.NewSQLite(tx), w.ID()
		},
	}
	for name, open := range impls {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws := open(t)
			older, _ := knowledgedomain.New(id.New(), ws, uuid.Nil, "older", knowledgedomain.Upload{Filename: "a.txt", Size: 3, SHA256: "x"}, now)
			d, _ := knowledgedomain.New(id.New(), ws, id.New(), "", knowledgedomain.Upload{Filename: "book.pdf", Size: 10, SHA256: "abc"}, now.Add(time.Minute))
			for _, doc := range []*knowledgedomain.Document{older, d} {
				if err := repo.Create(ctx, doc); err != nil {
					t.Fatal(err)
				}
			}
			pages := []knowledgedomain.Page{knowledgedomain.NewPage(1, "bir"), knowledgedomain.NewPage(2, "iki"), knowledgedomain.NewPage(3, "üç")}
			if err := repo.ReplacePages(ctx, d.ID(), pages); err != nil {
				t.Fatal(err)
			}
			_ = d.Extracted(pages, now.Add(2*time.Minute))
			if err := repo.Update(ctx, d); err != nil {
				t.Fatal(err)
			}
			got, err := repo.Get(ctx, ws, d.ID())
			if err != nil || got.Snapshot() != d.Snapshot() {
				t.Fatalf("get = %+v, %v", got, err)
			}
			if _, err := repo.Get(ctx, id.New(), d.ID()); !apperror.Is(err, apperror.DocumentNotFound) {
				t.Fatalf("other workspace err = %v", err)
			}
			list, _ := repo.List(ctx, ws)
			if len(list) != 2 || list[0].ID() != d.ID() {
				t.Fatalf("list = %d", len(list))
			}
			mid, _ := repo.Pages(ctx, d.ID(), 2, 3)
			all, _ := repo.Pages(ctx, d.ID(), 1, 0)
			if len(mid) != 2 || mid[0].Text != "iki" || len(all) != 3 {
				t.Fatalf("pages = %+v / %d", mid, len(all))
			}
			hs := []knowledgedomain.Heading{{Level: 1, Title: "Giriş", Page: 1}, {Level: 2, Title: "Kaygı", Page: 2}}
			if err := repo.ReplaceHeadings(ctx, d.ID(), hs); err != nil {
				t.Fatal(err)
			}
			if err := repo.ReplaceHeadings(ctx, d.ID(), hs[1:]); err != nil {
				t.Fatal(err)
			}
			if got, err := repo.Headings(ctx, d.ID()); err != nil || !reflect.DeepEqual(got, hs[1:]) {
				t.Fatalf("headings = %+v, %v", got, err)
			}
			if err := repo.Delete(ctx, ws, d.ID()); err != nil {
				t.Fatal(err)
			}
			if gone, _ := repo.Pages(ctx, d.ID(), 1, 0); len(gone) != 0 {
				t.Fatalf("pages survived delete: %d", len(gone))
			}
			if gone, _ := repo.Headings(ctx, d.ID()); len(gone) != 0 {
				t.Fatalf("headings survived delete: %d", len(gone))
			}
			if err := repo.Delete(ctx, ws, d.ID()); !apperror.Is(err, apperror.DocumentNotFound) {
				t.Fatalf("double delete = %v", err)
			}
		})
	}
}
