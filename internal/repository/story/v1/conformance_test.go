package story_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
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
			if back, _ := repo.Revision(ctx, video.ID(), 1); back.Preview != (storydomain.Render{}) {
				t.Fatalf("preview render before rendering = %+v", back.Preview)
			}
			rev.Preview = storydomain.Render{Scale: 2.0 / 3, FPS: 15}
			_ = repo.PutRevision(ctx, rev)
			back, _ := repo.Revision(ctx, video.ID(), 1)
			if back.Artifacts["preview.mp4"] != "p" || back.Artifacts["workdir.tar.gz"] != "k" || back.Notes != "notes" ||
				back.Measures != (storydomain.Measures{Duration: 47500 * time.Millisecond, Words: 140, Narrator: narrated.Narrator}) ||
				back.Preview != (storydomain.Render{Scale: 2.0 / 3, FPS: 15}) {
				t.Fatalf("revision = %+v", back)
			}
			if _, err := repo.Revision(ctx, video.ID(), 2); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("missing revision = %v", err)
			}

			// A note stays when the revision is written again, and a blank
			// one removes it.
			note, _ := storydomain.NewAnnotation("Müzik onaylandı", id.New(), now)
			if err := repo.Annotate(ctx, video.ID(), 1, note); err != nil {
				t.Fatal(err)
			}
			_ = repo.PutRevision(ctx, rev)
			if back, _ := repo.Revision(ctx, video.ID(), 1); back.Annotation != note || back.Artifacts["preview.mp4"] != "p" {
				t.Fatalf("annotated revision = %+v, want note %+v", back, note)
			}
			if err := repo.Annotate(ctx, video.ID(), 1, storydomain.Annotation{}); err != nil {
				t.Fatal(err)
			}
			if back, _ := repo.Revision(ctx, video.ID(), 1); !back.Annotation.IsZero() || back.Annotation.By != uuid.Nil {
				t.Fatalf("cleared note = %+v", back.Annotation)
			}
			if err := repo.Annotate(ctx, video.ID(), 2, note); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("annotating a missing revision = %v", err)
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

			// A revision's thumbnail variants and the deliverable's pick.
			thumbs, err := storydomain.ParseThumbnails([]byte(`{"recommended": 2, "variants": [
				{"n": 1, "concept": "frame", "text": "Kaygı mı?", "still": 12.4}, {"n": 2, "concept": "type", "text": "3 fark"}]}`), 2)
			if err != nil {
				t.Fatal(err)
			}
			rev.Thumbnails = thumbs
			_ = repo.PutRevision(ctx, rev)
			if back, _ := repo.Revision(ctx, video.ID(), 1); !reflect.DeepEqual(back.Thumbnails, thumbs) {
				t.Fatalf("thumbnails = %+v, want %+v", back.Thumbnails, thumbs)
			}
			if err := rendered.PickThumbnail(video.ID(), 1, thumbs, now); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, rendered); err != nil {
				t.Fatal(err)
			}
			if picked, _ := repo.Get(ctx, ws, s.ID()); picked.Lead().Thumbnail() != 1 {
				t.Fatalf("picked thumbnail = %d", picked.Lead().Thumbnail())
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

			inReview, _, _ := repo.List(ctx, ws, storyrepo.Filter{Statuses: []storydomain.Status{storydomain.StatusReview}})
			if len(inReview) != 1 {
				t.Fatalf("review list = %d", len(inReview))
			}
			other := newStory(t, ws, "Çocuklarda uyku düzeni", now.Add(time.Hour))
			_ = repo.Create(ctx, other)
			all, _, _ := repo.List(ctx, ws, storyrepo.Filter{})
			if len(all) != 2 || all[0].ID() != other.ID() {
				t.Fatalf("list = %d", len(all))
			}

			matches, err := repo.Similar(ctx, ws, "Kaygı ve endişe farkı nedir", 5)
			if err != nil || len(matches) == 0 || matches[0].StoryID != s.ID() || matches[0].Score < 0.3 {
				t.Fatalf("similar = %+v, %v", matches, err)
			}
			// The job publishing a deliverable is stored with it.
			pub, _ := repo.Get(ctx, ws, s.ID())
			_, _ = pub.Approve(video.ID(), id.New(), id.New, now)
			_ = pub.Rendered(video.ID(), 0, now)
			job := id.New()
			if err := pub.StartPublishing(video.ID(), job, now); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, pub); err != nil {
				t.Fatal(err)
			}
			if back, _ := repo.Get(ctx, ws, s.ID()); back.Lead().PublishJob() != job || back.Lead().Status() != storydomain.StatusPublishing {
				t.Fatalf("publishing job = %s (%s), want %s", back.Lead().PublishJob(), back.Lead().Status(), job)
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

func impls() map[string]func(t *testing.T) (storyrepo.Repository, uuid.UUID) {
	return map[string]func(t *testing.T) (storyrepo.Repository, uuid.UUID){
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
}

// List pages through every match exactly once, newest first, whatever
// the page size, and filters by title and origin.
func TestListPagesAndFilters(t *testing.T) {
	titles := []string{"Kaygı nedir", "Uyku düzeni", "KAYGI ve beden", "Panik atak", "Sınav kaygısı"}
	tests := []struct {
		name   string
		filter storyrepo.Filter
		want   []string // titles, newest first
	}{
		{name: "everything, two at a time", filter: storyrepo.Filter{Limit: 2},
			want: []string{"Sınav kaygısı", "Panik atak", "KAYGI ve beden", "Uyku düzeni", "Kaygı nedir"}},
		{name: "everything in one page", filter: storyrepo.Filter{Limit: 5},
			want: []string{"Sınav kaygısı", "Panik atak", "KAYGI ve beden", "Uyku düzeni", "Kaygı nedir"}},
		{name: "title search folds case and Turkish letters", filter: storyrepo.Filter{Query: "kaygi", Limit: 1},
			want: []string{"Sınav kaygısı", "KAYGI ve beden", "Kaygı nedir"}},
		{name: "by origin", filter: storyrepo.Filter{Origin: storydomain.OriginManual},
			want: []string{"Sınav kaygısı", "Panik atak", "KAYGI ve beden", "Uyku düzeni", "Kaygı nedir"}},
		{name: "another origin", filter: storyrepo.Filter{Origin: storydomain.OriginSchedule}, want: nil},
	}
	for name, open := range impls() {
		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				ctx := context.Background()
				repo, ws := open(t)
				for i, title := range titles {
					// Two stories share each instant, so ties go to the id.
					if err := repo.Create(ctx, newStory(t, ws, title, now.Add(time.Duration(i/2)*time.Minute))); err != nil {
						t.Fatal(err)
					}
				}
				var got []string
				f := tt.filter
				for range 10 {
					list, next, err := repo.List(ctx, ws, f)
					if err != nil {
						t.Fatal(err)
					}
					for _, st := range list {
						got = append(got, st.Idea().Title)
					}
					if next == "" {
						break
					}
					f.After = next
				}
				if !slices.Equal(got, tt.want) {
					t.Fatalf("titles = %q, want %q", got, tt.want)
				}
			})
		}
	}
	for name, open := range impls() {
		t.Run(name+"/bad cursor", func(t *testing.T) {
			repo, ws := open(t)
			if _, _, err := repo.List(context.Background(), ws, storyrepo.Filter{After: "nope"}); !apperror.Is(err, apperror.ValidationFailed) {
				t.Fatalf("bad cursor = %v", err)
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

// interruptedSource commits save between a load's first and second read,
// like a worker saving the story while a reviewer's page loads it.
type interruptedSource struct {
	*db.SQLiteTxManager
	reads int
	save  func()
}

func (s *interruptedSource) R(ctx context.Context) db.Querier {
	s.reads++
	if s.reads == 2 && s.save != nil {
		s.save()
	}
	return s.SQLiteTxManager.R(ctx)
}

// A story is loaded with the deliverables of the same save, never with
// those of a save that landed while it was being read.
func TestStoryLoadsFromOneSave(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, now)
	_ = workspacerepo.NewSQLite(tx).Create(ctx, w)
	writer := storyrepo.NewSQLite(tx)
	s := newStory(t, w.ID(), "Kaygı nedir", now)
	if err := writer.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	src := &interruptedSource{SQLiteTxManager: tx}
	src.save = func() {
		st, _ := writer.Get(ctx, w.ID(), s.ID())
		_, _ = st.StartDrafting(st.Lead().ID(), now)
		if err := writer.Save(ctx, st); err != nil {
			t.Error(err)
		}
	}
	got, err := storyrepo.NewSQLite(src).Get(ctx, w.ID(), s.ID())
	if err != nil {
		t.Fatal(err)
	}
	drafting := got.Lead().Status() == storydomain.StatusDrafting
	if drafting != (got.Version() == 2) {
		t.Fatalf("story v%d loaded with its lead %s", got.Version(), got.Lead().Status())
	}
}

// A story that reviews scripts first keeps when the script was approved
// and which revision held only the script.
func TestScriptReviewRoundTrip(t *testing.T) {
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
			v, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 45*time.Second)
			o, _ := pipelinedomain.NewOutputs(&v, nil, "")
			idea, _ := storydomain.NewIdea("Kaygı", "açı", nil)
			s, err := storydomain.New(id.New(), storydomain.Plan{
				WorkspaceID: ws, Origin: storydomain.OriginManual, Language: pipelinedomain.Turkish, Idea: idea, Outputs: o,
				Brief: storydomain.Brief{Production: pipelinedomain.Production{ReviewAt: pipelinedomain.ReviewScript}},
			}, id.New, now)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Create(ctx, s); err != nil {
				t.Fatal(err)
			}
			video := s.Lead()
			_, _ = s.StartDrafting(video.ID(), now)
			rev, _ := s.Drafted(video.ID(), map[string]string{"workdir.tar.gz": "k"}, "sess", "", storydomain.Measures{Words: 90}, now)
			if err := repo.PutRevision(ctx, rev); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Approve(video.ID(), id.New(), id.New, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, s); err != nil {
				t.Fatal(err)
			}
			back, _ := repo.Revision(ctx, video.ID(), 1)
			if back.Stage != storydomain.StageScript {
				t.Fatalf("revision stage = %q", back.Stage)
			}
			got, _ := repo.Get(ctx, ws, s.ID())
			d := got.Lead()
			if !d.ScriptApprovedAt().Equal(now.Add(time.Minute)) || d.Status() != storydomain.StatusQueued || got.AtScript(d.ID()) {
				t.Fatalf("lead = %s, script approved %v", d.Status(), d.ScriptApprovedAt())
			}
			if got.Brief().Production.ReviewAt != pipelinedomain.ReviewScript {
				t.Fatalf("brief = %+v", got.Brief().Production)
			}
		})
	}
}

func TestShortsRoundTrip(t *testing.T) {
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
	sec := func(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
	var segs []storydomain.Segment
	for s := 0.0; s < 600; s += 5 {
		segs = append(segs, storydomain.Segment{Start: sec(s), End: sec(s + 4.6)})
	}
	clip := func(start, end float64) storydomain.Clip {
		c, _ := storydomain.NewClip(sec(start), sec(end))
		return c
	}
	limits := storydomain.DefaultShortsLimits()
	for name, open := range impls {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws := open(t)
			v, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatLong, pipelinedomain.Horizontal, 10*time.Minute)
			o, _ := pipelinedomain.NewOutputs(&v, nil, "")
			idea, _ := storydomain.NewIdea("Duygu adlandırma", "açı", nil)
			s, err := storydomain.New(id.New(), storydomain.Plan{
				WorkspaceID: ws, Origin: storydomain.OriginManual, Language: pipelinedomain.Turkish, Idea: idea, Outputs: o,
			}, id.New, now)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Create(ctx, s); err != nil {
				t.Fatal(err)
			}
			long := s.Lead()
			_, _ = s.StartDrafting(long.ID(), now)
			_, _ = s.Drafted(long.ID(), nil, "", "", storydomain.Measures{}, now)
			_ = s.Rendered(long.ID(), 10*time.Minute, now)
			_, _ = s.Approve(long.ID(), id.New(), id.New, now)
			if err := s.Rendered(long.ID(), 10*time.Minute, now); err != nil {
				t.Fatal(err)
			}
			kept, err := s.SuggestMoments(long.ID(), []storydomain.MomentDraft{
				{Clip: clip(10, 40), Hook: "Öfkeyi adlandır", Reason: "Tek başına anlaşılıyor", Score: 90},
				{Clip: clip(200, 230), Score: 40},
			}, segs, limits, id.New, now)
			if err != nil || len(kept) != 2 {
				t.Fatalf("suggest = %d %v", len(kept), err)
			}
			made, err := s.AddShorts(long.ID(), []uuid.UUID{kept[0].ID()}, limits, id.New, now)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, s); err != nil {
				t.Fatal(err)
			}

			got, err := repo.Get(ctx, ws, s.ID())
			if err != nil {
				t.Fatal(err)
			}
			short, err := got.Deliverable(made[0].ID())
			if err != nil || short.Clip() == nil || *short.Clip() != kept[0].Clip() {
				t.Fatalf("short = %+v %v", short, err)
			}
			if got.Lead().Clip() != nil {
				t.Fatal("the long video came back with a clip")
			}
			moments := got.Moments(long.ID())
			if len(moments) != 2 {
				t.Fatalf("moments = %d, want 2", len(moments))
			}
			if m := moments[0]; m.ID() != kept[0].ID() || m.Hook() != "Öfkeyi adlandır" || m.Reason() != "Tek başına anlaşılıyor" ||
				m.Score() != 90 || m.Status() != storydomain.MomentMade || m.ShortID() != short.ID() || m.Clip() != kept[0].Clip() ||
				!m.CreatedAt().Equal(now) {
				t.Fatalf("moment = %+v", m)
			}

			// A new scan replaces the agent's open suggestion; the made
			// moment stays.
			if _, err := got.SuggestMoments(long.ID(), []storydomain.MomentDraft{{Clip: clip(400, 430), Score: 70}}, segs, limits, id.New, now); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, got); err != nil {
				t.Fatal(err)
			}
			again, _ := repo.Get(ctx, ws, s.ID())
			var starts []float64
			for _, m := range again.Moments(long.ID()) {
				starts = append(starts, m.Clip().Start().Seconds())
			}
			if want := []float64{10, 400}; !slices.Equal(starts, want) {
				t.Fatalf("moments start at %v, want %v", starts, want)
			}

			// A draft that moves the short's cut saves the new clip and
			// length over the stored ones.
			if _, err := again.StartDrafting(short.ID(), now); err != nil {
				t.Fatal(err)
			}
			if err := again.Recut(short.ID(), clip(5, 39.6), limits, now); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, again); err != nil {
				t.Fatal(err)
			}
			recut, _ := repo.Get(ctx, ws, s.ID())
			moved, _ := recut.Deliverable(short.ID())
			if *moved.Clip() != clip(5, 39.6) || moved.Video().Duration() != 35*time.Second || recut.Moments(long.ID())[0].Clip() != clip(5, 39.6) {
				t.Fatalf("after recut: clip %v, length %s", moved.Clip(), moved.Video().Duration())
			}
		})
	}
}
