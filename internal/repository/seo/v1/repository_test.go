package seo_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/internal/db/dbtest"
	seodomain "ekin/internal/domain/seo/v1"
	workspacedomain "ekin/internal/domain/workspace/v1"
	seorepo "ekin/internal/repository/seo/v1"
	workspacerepo "ekin/internal/repository/workspace/v1"
	"ekin/pkg/id"
)

var now = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func impls() map[string]func(t *testing.T) (seorepo.Repository, uuid.UUID) {
	return map[string]func(t *testing.T) (seorepo.Repository, uuid.UUID){
		"inmemory": func(*testing.T) (seorepo.Repository, uuid.UUID) { return seorepo.NewInMemory(), id.New() },
		"sqlite": func(t *testing.T) (seorepo.Repository, uuid.UUID) {
			_, tx := dbtest.Open(t)
			w, _ := workspacedomain.New(id.New(), "ws", time.UTC, now)
			if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
				t.Fatal(err)
			}
			return seorepo.NewSQLite(tx), w.ID()
		},
	}
}

// Save acts only on the status the caller read, so a keyword is never
// used twice and a dismissal never undoes a use.
func TestSaveGuardsTheStatusRead(t *testing.T) {
	story := id.New()
	tests := []struct {
		name    string
		stored  func(o *seodomain.Opportunity) // before the save
		change  func(o *seodomain.Opportunity)
		from    seodomain.Status
		wantErr apperror.Code
		want    seodomain.Status
	}{
		{name: "use an open one", change: use(story), from: seodomain.StatusOpen, want: seodomain.StatusUsed},
		{name: "use one another run used", stored: use(id.New()), change: use(story), from: seodomain.StatusOpen,
			wantErr: apperror.Conflict, want: seodomain.StatusUsed},
		{name: "dismiss one used meanwhile", stored: use(id.New()), change: dismiss, from: seodomain.StatusOpen,
			wantErr: apperror.Conflict, want: seodomain.StatusUsed},
		{name: "reopen a used one", stored: use(id.New()), change: reopen, from: seodomain.StatusUsed, want: seodomain.StatusOpen},
	}
	for name, open := range impls() {
		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				ctx := context.Background()
				repo, ws := open(t)
				o, err := seodomain.New(id.New(), ws, "kaygı nedir", "tr", 900, 20, "informational", "", "manual", now)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := repo.Add(ctx, o); err != nil {
					t.Fatal(err)
				}
				if tt.stored != nil {
					stored := o
					tt.stored(&stored)
					if err := repo.Save(ctx, stored, seodomain.StatusOpen); err != nil {
						t.Fatal(err)
					}
				}
				tt.change(&o)
				err = repo.Save(ctx, o, tt.from)
				if tt.wantErr != "" && !apperror.Is(err, tt.wantErr) || tt.wantErr == "" && err != nil {
					t.Fatalf("save = %v, want %q", err, tt.wantErr)
				}
				got, _ := repo.Get(ctx, ws, o.ID)
				if got.Status != tt.want {
					t.Fatalf("stored %s, want %s", got.Status, tt.want)
				}
			})
		}
	}
}

func use(story uuid.UUID) func(*seodomain.Opportunity) {
	return func(o *seodomain.Opportunity) { _ = o.Use(story, now) }
}
func dismiss(o *seodomain.Opportunity) { o.Dismiss(now) }
func reopen(o *seodomain.Opportunity)  { o.Reopen(now) }

// List pages through the pool once, open first then used then dismissed,
// best first within each, and filters by status, language and keyword.
func TestListPagesAndFilters(t *testing.T) {
	type kw struct {
		keyword, lang string
		volume, diff  int
		status        seodomain.Status
	}
	pool := []kw{
		{"kaygı nedir", "tr", 900, 20, seodomain.StatusOpen},          // 0: 900 × 1.0
		{"panik atak", "tr", 4000, 80, seodomain.StatusOpen},          // 1: 4000 × 0.4
		{"uyku hijyeni", "tr", 900, 20, seodomain.StatusOpen},         // 2: ties with 0
		{"sınav kaygısı", "tr", 5000, 10, seodomain.StatusUsed},       // 3: used, though best
		{"anxiety test", "en", 300, 30, seodomain.StatusOpen},         // 4
		{"KAYGI bozukluğu", "tr", 100, 50, seodomain.StatusDismissed}, // 5
	}
	tests := []struct {
		name   string
		filter seorepo.Filter
		want   []int // indexes into pool; ties go to the larger id, the later added
	}{
		{name: "everything, two at a time", filter: seorepo.Filter{Limit: 2}, want: []int{1, 2, 0, 4, 3, 5}},
		{name: "everything at once", filter: seorepo.Filter{}, want: []int{1, 2, 0, 4, 3, 5}},
		{name: "open Turkish, one at a time", filter: seorepo.Filter{Statuses: []seodomain.Status{seodomain.StatusOpen}, Language: "tr", Limit: 1},
			want: []int{1, 2, 0}},
		{name: "keyword search folds", filter: seorepo.Filter{Query: "Kaygi"}, want: []int{0, 3, 5}},
	}
	for name, open := range impls() {
		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				ctx := context.Background()
				repo, ws := open(t)
				ids := make([]uuid.UUID, len(pool))
				for i, k := range pool {
					o, err := seodomain.New(id.New(), ws, k.keyword, k.lang, k.volume, k.diff, "", "", "manual", now)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := repo.Add(ctx, o); err != nil {
						t.Fatal(err)
					}
					switch k.status {
					case seodomain.StatusUsed:
						use(id.New())(&o)
					case seodomain.StatusDismissed:
						dismiss(&o)
					}
					if k.status != seodomain.StatusOpen {
						if err := repo.Save(ctx, o, seodomain.StatusOpen); err != nil {
							t.Fatal(err)
						}
					}
					ids[i] = o.ID
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
					for _, o := range list {
						got = append(got, o.ID)
					}
					if next == "" {
						break
					}
					f.After = next
				}
				if !slices.Equal(got, want) {
					t.Fatalf("list = %v, want %v", got, want)
				}
				keywords, err := repo.Keywords(ctx, ws)
				if err != nil || len(keywords) != len(pool) {
					t.Fatalf("keywords = %v, %v", keywords, err)
				}
			})
		}
		t.Run(name+"/bad cursor", func(t *testing.T) {
			repo, ws := open(t)
			if _, _, err := repo.List(context.Background(), ws, seorepo.Filter{After: "nope"}); !apperror.Is(err, apperror.ValidationFailed) {
				t.Fatalf("bad cursor = %v", err)
			}
		})
	}
}
