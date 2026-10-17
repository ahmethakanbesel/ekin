package workspace_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"ekin/internal/db/dbtest"
	workspacedomain "ekin/internal/domain/workspace/v1"
	identityrepo "ekin/internal/repository/identity/v1"
	workspacerepo "ekin/internal/repository/workspace/v1"
	workspacesvc "ekin/internal/service/workspace/v1"
	"ekin/pkg/id"
)

// racingWorkspaces runs race once, right after the next Get.
type racingWorkspaces struct {
	workspacerepo.Repository
	race func()
}

func (r *racingWorkspaces) Get(ctx context.Context, id uuid.UUID) (*workspacedomain.Workspace, error) {
	w, err := r.Repository.Get(ctx, id)
	if race := r.race; race != nil {
		r.race = nil
		race()
	}
	return w, err
}

// Two people changing different fields of a workspace at once both keep
// theirs.
func TestConcurrentUpdatesBothLand(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	repo := &racingWorkspaces{Repository: workspacerepo.NewSQLite(tx)}
	svc := workspacesvc.NewService(repo, identityrepo.NewSQLite(tx), tx)
	w, _ := workspacedomain.New(id.New(), "acme", time.UTC, time.Now())
	if err := repo.Create(ctx, w); err != nil {
		t.Fatal(err)
	}
	ist, _ := time.LoadLocation("Europe/Istanbul")
	other := make(chan error, 1)
	repo.race = func() {
		go func() {
			_, err := svc.Update(context.Background(), w.ID(), workspacesvc.Change{Location: ist})
			other <- err
		}()
		select {
		case err := <-other:
			other <- err
		case <-time.After(300 * time.Millisecond):
		}
	}
	name := "Acme"
	if _, err := svc.Update(ctx, w.ID(), workspacesvc.Change{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if err := <-other; err != nil {
		t.Fatal(err)
	}
	got, _ := workspacerepo.NewSQLite(tx).Get(ctx, w.ID())
	if got.Name() != "Acme" || got.Location().String() != "Europe/Istanbul" {
		t.Fatalf("stored = %q in %s; want both changes", got.Name(), got.Location())
	}
}
