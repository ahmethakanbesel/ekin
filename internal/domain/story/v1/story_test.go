package story_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	story "kurgu/internal/domain/story/v1"
	"kurgu/pkg/id"
)

var now = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

func outputs(t *testing.T, video, article bool, lead pipelinedomain.Kind, translations ...pipelinedomain.Language) pipelinedomain.Outputs {
	t.Helper()
	var v *pipelinedomain.VideoSpec
	var a *pipelinedomain.ArticleSpec
	if video {
		spec, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 45*time.Second)
		v = &spec
	}
	if article {
		spec, _ := pipelinedomain.NewArticleSpec(900, translations)
		a = &spec
	}
	o, err := pipelinedomain.NewOutputs(v, a, lead)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func newStory(t *testing.T, o pipelinedomain.Outputs) *story.Story {
	t.Helper()
	idea, err := story.NewIdea("Kaygı ile endişe arasındaki fark", "Bedensel belirtiler üzerinden.", []string{"kaygı", "Kaygı", " endişe "})
	if err != nil {
		t.Fatal(err)
	}
	if len(idea.Keywords) != 2 {
		t.Fatalf("keywords not deduplicated: %v", idea.Keywords)
	}
	s, err := story.New(id.New(), story.Plan{
		WorkspaceID: id.New(), Origin: story.OriginSchedule, Language: pipelinedomain.Turkish,
		Idea: idea, Outputs: o,
	}, id.New, now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFingerprint(t *testing.T) {
	tests := map[string]string{
		"Kaygı İLE Endişe: Fark?":     "kaygi ile endise fark",
		"  Çocuklarda öfke -- nöbeti": "cocuklarda ofke nobeti",
		"Alâkalı kâr":                 "alakali kar",
	}
	for in, want := range tests {
		if got := story.Fingerprint(in); got != want {
			t.Errorf("Fingerprint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVideoLifecycle(t *testing.T) {
	s := newStory(t, outputs(t, true, true, pipelinedomain.KindVideo, pipelinedomain.English))
	ds := s.Deliverables()
	video, article := ds[0], ds[1]
	if video.Role() != story.RoleLead || video.Status() != story.StatusQueued || article.Status() != story.StatusWaiting || article.SourceID() != video.ID() {
		t.Fatalf("initial = %+v / %+v", video, article)
	}
	if _, err := s.StartDrafting(article.ID(), now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("derived drafted early: %v", err)
	}
	if fb, err := s.StartDrafting(video.ID(), now); err != nil || fb != nil {
		t.Fatalf("start = %v, %v", fb, err)
	}
	if _, err := s.StartDrafting(video.ID(), now); err != nil || video.Status() != story.StatusDrafting {
		t.Fatalf("restart an interrupted draft = %s, %v", video.Status(), err)
	}
	rev, err := s.Drafted(video.ID(), map[string]string{"workdir.tar.gz": "k1"}, "sess-1", "", story.Measures{}, now)
	if err != nil || rev.Number != 1 || video.Status() != story.StatusRendering || video.Session() != "sess-1" {
		t.Fatalf("drafted = %+v %s %v", rev, video.Status(), err)
	}
	if err := s.Rendered(video.ID(), 0, now); err != nil || video.Status() != story.StatusReview || s.Status() != story.StatusReview {
		t.Fatalf("rendered = %s %v", video.Status(), err)
	}

	at := 12.5
	fb, _ := story.NewFeedback([]story.Note{{Text: "Make the hook faster", Scope: story.ScopeScript, AtSeconds: &at}}, id.New(), now)
	if err := s.RequestChanges(video.ID(), fb, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.StartDrafting(video.ID(), now)
	if got == nil || len(got.Notes) != 1 || got.Notes[0].Text != "Make the hook faster" {
		t.Fatalf("feedback lost: %v", got)
	}
	rev, _ = s.Drafted(video.ID(), nil, "", "", story.Measures{}, now)
	if rev.Number != 2 || rev.Feedback == nil || video.PendingFeedback() != nil || video.Session() != "sess-1" {
		t.Fatalf("revision 2 = %+v", rev)
	}
	_ = s.Rendered(video.ID(), 0, now)

	queued, err := s.Approve(video.ID(), id.New(), id.New, now)
	if err != nil || video.Status() != story.StatusFinalizing || len(queued) != 0 || article.Status() != story.StatusWaiting {
		t.Fatalf("approve = %v %v", queued, err)
	}
	if _, err := s.ReleaseDerived(video.ID(), now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("released before the final render: %v", err)
	}
	_ = s.Rendered(video.ID(), 0, now)
	if video.Status() != story.StatusReady {
		t.Fatalf("final render = %s", video.Status())
	}
	if err := s.StartPublishing(video.ID(), id.New(), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Published(video.ID(), story.Publication{URL: "https://youtu.be/x", ExternalID: "x"}, now); err != nil || video.Status() != story.StatusPublished {
		t.Fatalf("published = %v", err)
	}
	queued, err = s.ReleaseDerived(video.ID(), now)
	if err != nil || len(queued) != 1 || queued[0].ID() != article.ID() || article.Status() != story.StatusQueued {
		t.Fatalf("release = %v %v", queued, err)
	}

	// The derived article: approval adds its English translation.
	_, _ = s.StartDrafting(article.ID(), now)
	_, _ = s.Drafted(article.ID(), nil, "", "", story.Measures{}, now)
	if article.Status() != story.StatusReview {
		t.Fatalf("article skips rendering: %s", article.Status())
	}
	queued, _ = s.Approve(article.ID(), id.New(), id.New, now)
	if article.Status() != story.StatusReady || len(queued) != 1 || queued[0].Role() != story.RoleTranslation || queued[0].Language() != pipelinedomain.English {
		t.Fatalf("translations = %+v", queued)
	}
	tr := queued[0]
	_, _ = s.StartDrafting(tr.ID(), now)
	_, _ = s.Drafted(tr.ID(), nil, "", "", story.Measures{}, now)
	if more, _ := s.Approve(tr.ID(), id.New(), id.New, now); len(more) != 0 {
		t.Fatalf("a translation spawned translations: %d", len(more))
	}

	back, err := story.Rehydrate(s.Snapshot())
	if err != nil || len(back.Deliverables()) != 3 {
		t.Fatalf("rehydrate = %v", err)
	}
	if back.Snapshot().Deliverables[0].Published.URL != "https://youtu.be/x" {
		t.Fatal("publication lost")
	}
}

func TestFailRetryAndReject(t *testing.T) {
	s := newStory(t, outputs(t, true, true, pipelinedomain.KindVideo))
	video := s.Lead()
	_, _ = s.StartDrafting(video.ID(), now)
	_, _ = s.Drafted(video.ID(), nil, "", "", story.Measures{}, now)
	if err := s.Fail(video.ID(), "chrome crashed", now); err != nil {
		t.Fatal(err)
	}
	if s.Status() != story.StatusFailed || video.FailedStep() != story.StatusRendering {
		t.Fatalf("failed = %s / %s", s.Status(), video.FailedStep())
	}
	next, err := s.Retry(video.ID(), now)
	if err != nil || next != story.StatusRendering || video.Failure() != "" {
		t.Fatalf("retry = %s %v", next, err)
	}
	_ = s.Rendered(video.ID(), 0, now)
	if err := s.Reject(video.ID(), now); err != nil {
		t.Fatal(err)
	}
	for _, d := range s.Deliverables() {
		if d.Status() != story.StatusRejected {
			t.Fatalf("%s left %s after the lead was rejected", d.Kind(), d.Status())
		}
	}
	if err := s.Fail(video.ID(), "x", now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("fail rejected = %v", err)
	}
}

// readyArticle returns a story whose article lead is approved and ready
// to publish.
func readyArticle(t *testing.T) (*story.Story, *story.Deliverable) {
	t.Helper()
	s := newStory(t, outputs(t, false, true, ""))
	article := s.Lead()
	_, _ = s.StartDrafting(article.ID(), now)
	_, _ = s.Drafted(article.ID(), nil, "", "", story.Measures{}, now)
	if _, err := s.Approve(article.ID(), id.New(), id.New, now); err != nil || article.Status() != story.StatusReady {
		t.Fatalf("approve = %s %v", article.Status(), err)
	}
	return s, article
}

func TestStartPublishingBelongsToOneJob(t *testing.T) {
	first, second := id.New(), id.New()
	tests := []struct {
		name    string
		starts  []uuid.UUID // earlier StartPublishing calls, in order
		job     uuid.UUID
		wantErr apperror.Code
	}{
		{name: "ready", job: first},
		{name: "the same job resumes its cut-short attempt", starts: []uuid.UUID{first}, job: first},
		{name: "another job is refused", starts: []uuid.UUID{first}, job: second, wantErr: apperror.InvalidTransition},
		{name: "no job", job: uuid.Nil, wantErr: apperror.ValidationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, article := readyArticle(t)
			for _, j := range tt.starts {
				if err := s.StartPublishing(article.ID(), j, now); err != nil {
					t.Fatal(err)
				}
			}
			err := s.StartPublishing(article.ID(), tt.job, now)
			if tt.wantErr != "" {
				if !apperror.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %s", err, tt.wantErr)
				}
				return
			}
			if err != nil || article.Status() != story.StatusPublishing || article.PublishJob() != tt.job {
				t.Fatalf("start = %v, %s by %s", err, article.Status(), article.PublishJob())
			}
			if back, err := story.Rehydrate(s.Snapshot()); err != nil || back.Lead().PublishJob() != tt.job {
				t.Fatalf("publishing job not kept: %v", err)
			}
		})
	}
}

func TestMeasuresFollowTheLatestRevision(t *testing.T) {
	s := newStory(t, outputs(t, true, false, pipelinedomain.KindVideo))
	video := s.Lead()
	editor := id.New()
	check := func(step string, rev story.Revision, want story.Measures) {
		t.Helper()
		if rev.Measures != want || video.Measures() != want {
			t.Fatalf("%s: revision %+v, deliverable %+v, want %+v", step, rev.Measures, video.Measures(), want)
		}
	}

	_, _ = s.StartDrafting(video.ID(), now)
	ema := story.Narrator{Engine: "ema", Voice: "ema"}
	rev, _ := s.Drafted(video.ID(), nil, "", "", story.Measures{Duration: time.Hour, Words: 112, Narrator: ema}, now)
	check("drafted", rev, story.Measures{Words: 112, Narrator: ema}) // the length comes from the render
	_ = s.Rendered(video.ID(), 0, now)                               // not measured: nothing changes
	check("unmeasured render", rev, story.Measures{Words: 112, Narrator: ema})

	fb, _ := story.NewFeedback([]story.Note{{Text: "Shorter", Scope: story.ScopeScript}}, editor, now)
	_ = s.RequestChanges(video.ID(), fb, now)
	_, _ = s.StartDrafting(video.ID(), now)
	eleven := story.Narrator{Engine: "elevenlabs", Voice: "JBFqnCBsd6RMkjVDRZzb", Model: "eleven_multilingual_v2"}
	rev, _ = s.Drafted(video.ID(), nil, "", "", story.Measures{Words: 120, Narrator: eleven}, now)
	check("redrafted", rev, story.Measures{Words: 120, Narrator: eleven})
	_ = s.Rendered(video.ID(), 47*time.Second, now)
	if want := (story.Measures{Duration: 47 * time.Second, Words: 120, Narrator: eleven}); video.Measures() != want {
		t.Fatalf("rendered = %+v, want %+v", video.Measures(), want)
	}

	// A metadata edit keeps the picture, so the length carries over.
	rev, _ = s.Edited(video.ID(), nil, editor, nil, 120, now)
	check("metadata edit", rev, story.Measures{Duration: 47 * time.Second, Words: 120, Narrator: eleven})

	// A narration edit goes back to the agent: the length is unknown
	// until the next render; the voice stays until it reads it again.
	retime, _ := story.NewFeedback([]story.Note{{Text: "re-time", Scope: story.ScopeScript}}, editor, now)
	rev, _ = s.Edited(video.ID(), nil, editor, &retime, 98, now)
	check("narration edit", rev, story.Measures{Words: 98, Narrator: eleven})

	back, err := story.Rehydrate(s.Snapshot())
	if err != nil || back.Lead().Measures() != (story.Measures{Words: 98, Narrator: eleven}) {
		t.Fatalf("rehydrated measures = %+v, %v", back.Lead().Measures(), err)
	}
}

func TestNewFeedback(t *testing.T) {
	neg, at3, at9 := -1.0, 3.0, 9.0
	many := make([]story.Note, story.MaxFeedbackNotes+1)
	for i := range many {
		many[i] = story.Note{Text: "ok"}
	}
	tests := []struct {
		name  string
		notes []story.Note
		ok    bool
		// The texts in the order kept, when ok.
		order []string
	}{
		{"one note", []story.Note{{Text: "ok"}}, true, []string{"ok"}},
		{"no notes", nil, false, nil},
		{"blank note", []story.Note{{Text: "ok"}, {Text: "  ", Scope: story.ScopeAll}}, false, nil},
		{"unknown scope", []story.Note{{Text: "ok", Scope: story.Scope("tone")}}, false, nil},
		{"negative time", []story.Note{{Text: "ok", Scope: story.ScopeVisuals, AtSeconds: &neg}}, false, nil},
		{"too long", []story.Note{{Text: strings.Repeat("a", story.MaxFeedbackRunes+1)}}, false, nil},
		{"too many", many, false, nil},
		{"at the limit", many[:story.MaxFeedbackNotes], true, nil},
		{
			"untimed first, then by time",
			[]story.Note{{Text: "late", AtSeconds: &at9}, {Text: "whole"}, {Text: "early", AtSeconds: &at3}, {Text: "tone"}},
			true, []string{"whole", "tone", "early", "late"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb, err := story.NewFeedback(tt.notes, uuid.Nil, now)
			if (err == nil) != tt.ok {
				t.Fatalf("NewFeedback = %v, want ok %v", err, tt.ok)
			}
			if err != nil {
				return
			}
			for _, n := range fb.Notes {
				if n.Scope == "" {
					t.Error("empty scope not defaulted")
				}
			}
			if tt.order != nil {
				got := make([]string, len(fb.Notes))
				for i, n := range fb.Notes {
					got[i] = n.Text
				}
				if !slices.Equal(got, tt.order) {
					t.Errorf("order = %v, want %v", got, tt.order)
				}
			}
		})
	}
}

// Feedback stored before it carried several notes reads as one note.
func TestFeedbackJSON(t *testing.T) {
	by := uuid.MustParse("01a14343-5400-7c50-a726-d5f8ab86553c")
	tests := []struct {
		name  string
		raw   string
		notes []story.Note
	}{
		{
			"legacy single note",
			`{"text":"Slow the voice down","scope":"voice","atSeconds":21.5,"by":"` + by.String() + `","at":"2026-10-13T09:00:00Z"}`,
			[]story.Note{{Text: "Slow the voice down", Scope: story.ScopeVoice, AtSeconds: ptr(21.5)}},
		},
		{
			"legacy without a time or scope",
			`{"text":"Shorter","by":"` + by.String() + `","at":"2026-10-13T09:00:00Z"}`,
			[]story.Note{{Text: "Shorter", Scope: story.ScopeAll}},
		},
		{
			"notes",
			`{"notes":[{"text":"Tighter intro","scope":"all"},{"text":"Wrong chart","scope":"visuals","atSeconds":14.2}],"by":"` + by.String() + `","at":"2026-10-13T09:00:00Z"}`,
			[]story.Note{{Text: "Tighter intro", Scope: story.ScopeAll}, {Text: "Wrong chart", Scope: story.ScopeVisuals, AtSeconds: ptr(14.2)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fb story.Feedback
			if err := json.Unmarshal([]byte(tt.raw), &fb); err != nil {
				t.Fatal(err)
			}
			if fb.By != by || !fb.At.Equal(now) {
				t.Fatalf("by, at = %v, %v", fb.By, fb.At)
			}
			if !slices.EqualFunc(fb.Notes, tt.notes, sameNote) {
				t.Fatalf("notes = %+v, want %+v", fb.Notes, tt.notes)
			}
			// It writes the new shape and reads it back the same.
			raw, _ := json.Marshal(fb)
			var back story.Feedback
			if err := json.Unmarshal(raw, &back); err != nil || !slices.EqualFunc(back.Notes, tt.notes, sameNote) {
				t.Fatalf("round trip %s = %+v, %v", raw, back.Notes, err)
			}
		})
	}
}

func sameNote(a, b story.Note) bool {
	return a.Text == b.Text && a.Scope == b.Scope &&
		(a.AtSeconds == nil) == (b.AtSeconds == nil) && (a.AtSeconds == nil || *a.AtSeconds == *b.AtSeconds)
}

func ptr[T any](v T) *T { return &v }
