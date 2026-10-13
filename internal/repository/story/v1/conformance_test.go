package story_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
)

var now = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

func newStory(t *testing.T, ws uuid.UUID, title string, at time.Time) *storydomain.Story {
	t.Helper()
	v, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 45*time.Second)
	a, _ := pipelinedomain.NewArticleSpec(900, []pipelinedomain.Language{pipelinedomain.English})
	o, _ := pipelinedomain.NewOutputs(&v, &a, pipelinedomain.KindVideo)
	idea, _ := storydomain.NewIdea(title, "açı", []string{"k1"})
	s, err := storydomain.New(id.New(), storydomain.Plan{
		WorkspaceID: ws, Origin: storydomain.OriginManual, Language: pipelinedomain.Turkish, Idea: idea,
		Brief: storydomain.Brief{Instructions: "x", Refs: []storydomain.Ref{{DocumentID: id.New(), From: 1, To: 3}}}, Outputs: o,
	}, id.New, at)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestConformance(t *testing.T) {
	impls := map[string]func(t *testing.T) (storyrepo.Repository, uuid.UUID){
		"inmemory": func(*testing.T) (storyrepo.Repository, uuid.UUID) { return storyrepo.NewInMemory(), id.New() },
		"sqlite": func(t *testing.T) (storyrepo.Repository, uuid.UUID) {
			_, tx := dbtest.Open(t)
			w, _ := workspacedomain.New(id.New(), "ws", time.UTC, now)
			if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
				t.Fatal(err)
			}
			return storyrepo.NewSQLite(tx), w.ID()
		},
	}
	for name, open := range impls {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws := open(t)
			s := newStory(t, ws, "Kaygı ile endişe arasındaki fark", now)
			if err := repo.Create(ctx, s); err != nil {
				t.Fatal(err)
			}
			got, err := repo.Get(ctx, ws, s.ID())
			if err != nil {
				t.Fatal(err)
			}
			if got.Idea().Title != s.Idea().Title || len(got.Deliverables()) != 2 || got.Brief().Refs[0].To != 3 || got.Version() != 1 {
				t.Fatalf("round trip = %+v", got.Snapshot())
			}

			video := got.Lead()
			_, _ = got.StartDrafting(video.ID(), now)
			rev, _ := got.Drafted(video.ID(), map[string]string{"workdir.tar.gz": "k"}, "sess", "notes", now)
			if err := repo.Save(ctx, got); err != nil {
				t.Fatal(err)
			}
			if err := repo.PutRevision(ctx, rev); err != nil {
				t.Fatal(err)
			}
			rev.Artifacts["preview.mp4"] = "p"
			_ = repo.PutRevision(ctx, rev)
			back, _ := repo.Revision(ctx, video.ID(), 1)
			if back.Artifacts["preview.mp4"] != "p" || back.Artifacts["workdir.tar.gz"] != "k" || back.Notes != "notes" {
				t.Fatalf("revision = %+v", back)
			}
			if _, err := repo.Revision(ctx, video.ID(), 2); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("missing revision = %v", err)
			}

			stale, _ := repo.Get(ctx, ws, s.ID())
			if stale.Version() != 2 || stale.Lead().Status() != storydomain.StatusRendering || stale.Lead().Session() != "sess" {
				t.Fatalf("saved = v%d %s", stale.Version(), stale.Lead().Status())
			}
			_ = got.Rendered(video.ID(), now)
			if err := repo.Save(ctx, got); err != nil {
				t.Fatal(err)
			}
			_ = stale.Rendered(video.ID(), now)
			if err := repo.Save(ctx, stale); !apperror.Is(err, apperror.StaleVersion) {
				t.Fatalf("stale save = %v", err)
			}

			inReview, _ := repo.List(ctx, ws, storyrepo.Filter{Statuses: []storydomain.Status{storydomain.StatusReview}})
			if len(inReview) != 1 {
				t.Fatalf("review list = %d", len(inReview))
			}
			other := newStory(t, ws, "Çocuklarda uyku düzeni", now.Add(time.Hour))
			_ = repo.Create(ctx, other)
			all, _ := repo.List(ctx, ws, storyrepo.Filter{})
			if len(all) != 2 || all[0].ID() != other.ID() {
				t.Fatalf("list = %d", len(all))
			}

			matches, err := repo.Similar(ctx, ws, "Kaygı ve endişe farkı nedir", 5)
			if err != nil || len(matches) == 0 || matches[0].StoryID != s.ID() || matches[0].Score < 0.3 {
				t.Fatalf("similar = %+v, %v", matches, err)
			}
			if err := repo.Delete(ctx, ws, s.ID()); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Get(ctx, ws, s.ID()); !apperror.Is(err, apperror.StoryNotFound) {
				t.Fatalf("deleted get = %v", err)
			}
		})
	}
}
