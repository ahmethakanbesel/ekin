package knowledge_test

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/internal/db/dbtest"
	knowledgedomain "ekin/internal/domain/knowledge/v1"
	workspacedomain "ekin/internal/domain/workspace/v1"
	knowledgerepo "ekin/internal/repository/knowledge/v1"
	workspacerepo "ekin/internal/repository/workspace/v1"
	"ekin/pkg/id"
)

var now = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

func impls() map[string]func(t *testing.T) (knowledgerepo.Repository, uuid.UUID) {
	return map[string]func(t *testing.T) (knowledgerepo.Repository, uuid.UUID){
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
}

func TestConformance(t *testing.T) {
	for name, open := range impls() {
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
			list, _, _ := repo.List(ctx, ws, knowledgerepo.Filter{})
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

// List pages through every match exactly once, newest first, and filters
// by status, format and title.
func TestListPagesAndFilters(t *testing.T) {
	docs := []struct{ title, file string }{
		{"Kaygı El Kitabı", "a.pdf"}, {"Uyku notları", "b.md"}, {"KAYGI ve beden", "c.txt"}, {"Panik", "d.pdf"}, {"Sınav kaygısı", "e.epub"},
	}
	tests := []struct {
		name   string
		filter knowledgerepo.Filter
		want   []int // indexes into docs, newest first
	}{
		{name: "everything, two at a time", filter: knowledgerepo.Filter{Limit: 2}, want: []int{4, 3, 2, 1, 0}},
		{name: "title search folds case and Turkish letters", filter: knowledgerepo.Filter{Query: "kaygi", Limit: 1}, want: []int{4, 2, 0}},
		{name: "pdfs", filter: knowledgerepo.Filter{Formats: []knowledgedomain.Format{knowledgedomain.FormatPDF}}, want: []int{3, 0}},
		{name: "ready ones", filter: knowledgerepo.Filter{Statuses: []knowledgedomain.Status{knowledgedomain.StatusReady}}, want: []int{1}},
		{name: "pending kaygı pdfs", filter: knowledgerepo.Filter{
			Query: "KAYGI", Formats: []knowledgedomain.Format{knowledgedomain.FormatPDF}, Statuses: []knowledgedomain.Status{knowledgedomain.StatusPending},
		}, want: []int{0}},
	}
	for name, open := range impls() {
		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				ctx := context.Background()
				repo, ws := open(t)
				ids := make([]uuid.UUID, len(docs))
				for i, doc := range docs {
					// Two share each instant, so ties go to the id.
					d, err := knowledgedomain.New(id.New(), ws, uuid.Nil, doc.title, knowledgedomain.Upload{Filename: doc.file, Size: 3, SHA256: "x"},
						now.Add(time.Duration(i/2)*time.Minute))
					if err != nil {
						t.Fatal(err)
					}
					if err := repo.Create(ctx, d); err != nil {
						t.Fatal(err)
					}
					if i == 1 {
						_ = d.Extracted([]knowledgedomain.Page{knowledgedomain.NewPage(1, "uyku")}, now)
						if err := repo.Update(ctx, d); err != nil {
							t.Fatal(err)
						}
					}
					ids[i] = d.ID()
				}
				var got, want []uuid.UUID
				for _, i := range tt.want {
					want = append(want, ids[i])
				}
				f := tt.filter
				for range 10 {
					list, next, err := repo.List(ctx, ws, f)
					if err != nil {
						t.Fatal(err)
					}
					for _, d := range list {
						got = append(got, d.ID())
					}
					if next == "" {
						break
					}
					f.After = next
				}
				if !slices.Equal(got, want) {
					t.Fatalf("list = %v, want %v", got, want)
				}
			})
		}
		t.Run(name+"/bad cursor", func(t *testing.T) {
			repo, ws := open(t)
			if _, _, err := repo.List(context.Background(), ws, knowledgerepo.Filter{After: "nope"}); !apperror.Is(err, apperror.ValidationFailed) {
				t.Fatalf("bad cursor = %v", err)
			}
		})
	}
}
