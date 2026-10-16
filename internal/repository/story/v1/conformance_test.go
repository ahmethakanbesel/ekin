package story_test

import (
	"context"
	"fmt"
	"slices"
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
			narrated := storydomain.Measures{Words: 140, Narrator: storydomain.Narrator{Engine: "elevenlabs", Voice: "v1", Model: "eleven_v3"}}
			_, _ = got.StartDrafting(video.ID(), now)
			rev, _ := got.Drafted(video.ID(), map[string]string{"workdir.tar.gz": "k"}, "sess", "notes", narrated, now)
			if err := repo.Save(ctx, got); err != nil {
				t.Fatal(err)
			}
			if err := repo.PutRevision(ctx, rev); err != nil {
				t.Fatal(err)
			}
			rev.Artifacts["preview.mp4"] = "p"
			rev.Measures.Duration = 47500 * time.Millisecond
			_ = repo.PutRevision(ctx, rev)
			back, _ := repo.Revision(ctx, video.ID(), 1)
			if back.Artifacts["preview.mp4"] != "p" || back.Artifacts["workdir.tar.gz"] != "k" || back.Notes != "notes" ||
				back.Measures != (storydomain.Measures{Duration: 47500 * time.Millisecond, Words: 140, Narrator: narrated.Narrator}) {
				t.Fatalf("revision = %+v", back)
			}
			if _, err := repo.Revision(ctx, video.ID(), 2); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("missing revision = %v", err)
			}

			stale, _ := repo.Get(ctx, ws, s.ID())
			if stale.Version() != 2 || stale.Lead().Status() != storydomain.StatusRendering || stale.Lead().Session() != "sess" {
				t.Fatalf("saved = v%d %s", stale.Version(), stale.Lead().Status())
			}
			_ = got.Rendered(video.ID(), 47500*time.Millisecond, now)
			if err := repo.Save(ctx, got); err != nil {
				t.Fatal(err)
			}
			rendered, _ := repo.Get(ctx, ws, s.ID())
			if m := rendered.Lead().Measures(); m != (storydomain.Measures{Duration: 47500 * time.Millisecond, Words: 140, Narrator: narrated.Narrator}) {
				t.Fatalf("deliverable measures = %+v", m)
			}
			_ = stale.Rendered(video.ID(), 0, now)
			if err := repo.Save(ctx, stale); !apperror.Is(err, apperror.StaleVersion) {
				t.Fatalf("stale save = %v", err)
			}

			// A round of several notes is stored whole, pending and on
			// the revision it asked for.
			at := 14.2
			fb, _ := storydomain.NewFeedback([]storydomain.Note{
				{Text: "Grafik yanlış", Scope: storydomain.ScopeVisuals, AtSeconds: &at},
				{Text: "Girişi kısalt"},
			}, id.New(), now)
			wantNotes := []string{"Girişi kısalt|all|-", "Grafik yanlış|visuals|14.2"}
			sent, _ := repo.Get(ctx, ws, s.ID())
			if err := sent.RequestChanges(video.ID(), fb, now); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, sent); err != nil {
				t.Fatal(err)
			}
			queued, _ := repo.Get(ctx, ws, s.ID())
			if got := noteKeys(queued.Lead().PendingFeedback()); !slices.Equal(got, wantNotes) {
				t.Fatalf("pending notes = %v, want %v", got, wantNotes)
			}
			_, _ = queued.StartDrafting(video.ID(), now)
			rev2, _ := queued.Drafted(video.ID(), map[string]string{"workdir.tar.gz": "k2"}, "sess", "", storydomain.Measures{}, now)
			_ = queued.Rendered(video.ID(), 0, now)
			if err := repo.Save(ctx, queued); err != nil {
				t.Fatal(err)
			}
			if err := repo.PutRevision(ctx, rev2); err != nil {
				t.Fatal(err)
			}
			back2, _ := repo.Revision(ctx, video.ID(), 2)
			if got := noteKeys(back2.Feedback); !slices.Equal(got, wantNotes) || back2.Feedback.By != fb.By {
				t.Fatalf("revision notes = %v, want %v", got, wantNotes)
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

// noteKeys is each note as "text|scope|time" ("-" for no time).
func noteKeys(fb *storydomain.Feedback) []string {
	if fb == nil {
		return nil
	}
	out := make([]string, 0, len(fb.Notes))
	for _, n := range fb.Notes {
		at := "-"
		if n.AtSeconds != nil {
			at = fmt.Sprint(*n.AtSeconds)
		}
		out = append(out, fmt.Sprintf("%s|%s|%s", n.Text, n.Scope, at))
	}
	return out
}

// Rows written before feedback carried several notes read as one note.
func TestSQLiteReadsSingleNoteFeedback(t *testing.T) {
	ctx := context.Background()
	d, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, now)
	if err := workspacerepo.NewSQLite(tx).Create(ctx, w); err != nil {
		t.Fatal(err)
	}
	repo := storyrepo.NewSQLite(tx)
	s := newStory(t, w.ID(), "Kaygı", now)
	if err := repo.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	video := s.Lead()
	_, _ = s.StartDrafting(video.ID(), now)
	rev, _ := s.Drafted(video.ID(), map[string]string{"workdir.tar.gz": "k"}, "sess", "", storydomain.Measures{}, now)
	rev.Number = 2
	if err := repo.PutRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	legacy := `{"text":"Kancayı hızlandır","scope":"script","atSeconds":4,"by":"01a14343-5400-7c50-a726-d5f8ab86553c","at":"2026-10-13T09:00:00Z"}`
	if _, err := d.Writer.ExecContext(ctx, `UPDATE deliverables SET pending = ? WHERE id = ?`, legacy, video.ID().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Writer.ExecContext(ctx, `UPDATE revisions SET feedback = ? WHERE deliverable_id = ?`, legacy, video.ID().String()); err != nil {
		t.Fatal(err)
	}
	want := []string{"Kancayı hızlandır|script|4"}
	got, err := repo.Get(ctx, w.ID(), s.ID())
	if err != nil {
		t.Fatal(err)
	}
	if keys := noteKeys(got.Lead().PendingFeedback()); !slices.Equal(keys, want) {
		t.Fatalf("pending = %v, want %v", keys, want)
	}
	back, _ := repo.Revision(ctx, video.ID(), 2)
	if keys := noteKeys(back.Feedback); !slices.Equal(keys, want) || back.Feedback.By.String() != "01a14343-5400-7c50-a726-d5f8ab86553c" {
		t.Fatalf("revision = %v, want %v", keys, want)
	}
}
