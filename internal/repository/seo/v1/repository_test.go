package seo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	seodomain "kurgu/internal/domain/seo/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	seorepo "kurgu/internal/repository/seo/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
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
