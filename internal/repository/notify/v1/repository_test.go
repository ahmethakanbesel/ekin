package notify_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db/dbtest"
	workspacedomain "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	notifyrepo "github.com/ahmethakanbesel/ekin/internal/repository/notify/v1"
	workspacerepo "github.com/ahmethakanbesel/ekin/internal/repository/workspace/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

var now = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func impls() map[string]func(t *testing.T) (notifyrepo.Repository, uuid.UUID) {
	return map[string]func(t *testing.T) (notifyrepo.Repository, uuid.UUID){
		"inmemory": func(*testing.T) (notifyrepo.Repository, uuid.UUID) { return notifyrepo.NewInMemory(), id.New() },
		"sqlite": func(t *testing.T) (notifyrepo.Repository, uuid.UUID) {
			_, tx := dbtest.Open(t)
			w, _ := workspacedomain.New(id.New(), "ws", time.UTC, now)
			if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
				t.Fatal(err)
			}
			return notifyrepo.NewSQLite(tx), w.ID()
		},
	}
}

// Feed pages through every match exactly once, newest first, and
// filters by kind.
func TestFeedPagesAndFilters(t *testing.T) {
	kinds := []string{"run", "review", "failed", "review", "published"}
	tests := []struct {
		name   string
		filter notifyrepo.FeedFilter
		want   []int // indexes into kinds, newest first
	}{
		{name: "everything, two at a time", filter: notifyrepo.FeedFilter{Limit: 2}, want: []int{4, 3, 2, 1, 0}},
		{name: "everything at once", filter: notifyrepo.FeedFilter{Limit: 5}, want: []int{4, 3, 2, 1, 0}},
		{name: "reviews, one at a time", filter: notifyrepo.FeedFilter{Kinds: []string{"review"}, Limit: 1}, want: []int{3, 1}},
		{name: "failures and publications", filter: notifyrepo.FeedFilter{Kinds: []string{"failed", "published"}}, want: []int{4, 2}},
	}
	for name, open := range impls() {
		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				ctx := context.Background()
				repo, ws := open(t)
				ids := make([]uuid.UUID, len(kinds))
				for i, k := range kinds {
					// Two share each instant, so ties go to the id.
					ids[i] = id.New()
					n := notifyrepo.Notification{ID: ids[i], WorkspaceID: ws, Kind: k, Title: k, CreatedAt: now.Add(time.Duration(i/2) * time.Minute)}
					if err := repo.Add(ctx, n); err != nil {
						t.Fatal(err)
					}
				}
				var got, want []uuid.UUID
				for _, i := range tt.want {
					want = append(want, ids[i])
				}
				f := tt.filter
				for range 10 {
					list, next, err := repo.Feed(ctx, ws, f)
					if err != nil {
						t.Fatal(err)
					}
					for _, n := range list {
						got = append(got, n.ID)
					}
					if next == "" {
						break
					}
					f.After = next
				}
				if !slices.Equal(got, want) {
					t.Fatalf("feed = %v, want %v", got, want)
				}
			})
		}
		t.Run(name+"/bad cursor", func(t *testing.T) {
			repo, ws := open(t)
			if _, _, err := repo.Feed(context.Background(), ws, notifyrepo.FeedFilter{After: "nope"}); !apperror.Is(err, apperror.ValidationFailed) {
				t.Fatalf("bad cursor = %v", err)
			}
		})
	}
}
