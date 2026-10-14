package story_test

import (
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
	rev, err := s.Drafted(video.ID(), map[string]string{"workdir.tar.gz": "k1"}, "sess-1", "", now)
	if err != nil || rev.Number != 1 || video.Status() != story.StatusRendering || video.Session() != "sess-1" {
		t.Fatalf("drafted = %+v %s %v", rev, video.Status(), err)
	}
	if err := s.Rendered(video.ID(), now); err != nil || video.Status() != story.StatusReview || s.Status() != story.StatusReview {
		t.Fatalf("rendered = %s %v", video.Status(), err)
	}

	at := 12.5
	fb, _ := story.NewFeedback("Make the hook faster", story.ScopeScript, &at, id.New(), now)
	if err := s.RequestChanges(video.ID(), fb, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.StartDrafting(video.ID(), now)
	if got == nil || got.Text != "Make the hook faster" {
		t.Fatalf("feedback lost: %v", got)
	}
	rev, _ = s.Drafted(video.ID(), nil, "", "", now)
	if rev.Number != 2 || rev.Feedback == nil || video.PendingFeedback() != nil || video.Session() != "sess-1" {
		t.Fatalf("revision 2 = %+v", rev)
	}
	_ = s.Rendered(video.ID(), now)

	queued, err := s.Approve(video.ID(), id.New(), id.New, now)
	if err != nil || video.Status() != story.StatusFinalizing || len(queued) != 0 || article.Status() != story.StatusWaiting {
		t.Fatalf("approve = %v %v", queued, err)
	}
	if _, err := s.ReleaseDerived(video.ID(), now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("released before the final render: %v", err)
	}
	_ = s.Rendered(video.ID(), now)
	if video.Status() != story.StatusReady {
		t.Fatalf("final render = %s", video.Status())
	}
	if err := s.StartPublishing(video.ID(), now); err != nil {
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
	_, _ = s.Drafted(article.ID(), nil, "", "", now)
	if article.Status() != story.StatusReview {
		t.Fatalf("article skips rendering: %s", article.Status())
	}
	queued, _ = s.Approve(article.ID(), id.New(), id.New, now)
	if article.Status() != story.StatusReady || len(queued) != 1 || queued[0].Role() != story.RoleTranslation || queued[0].Language() != pipelinedomain.English {
		t.Fatalf("translations = %+v", queued)
	}
	tr := queued[0]
	_, _ = s.StartDrafting(tr.ID(), now)
	_, _ = s.Drafted(tr.ID(), nil, "", "", now)
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
	_, _ = s.Drafted(video.ID(), nil, "", "", now)
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
	_ = s.Rendered(video.ID(), now)
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

func TestNewFeedback(t *testing.T) {
	neg := -1.0
	tests := []struct {
		text  string
		scope story.Scope
		at    *float64
		ok    bool
	}{
		{"ok", "", nil, true},
		{"  ", story.ScopeAll, nil, false},
		{"ok", story.Scope("tone"), nil, false},
		{"ok", story.ScopeVisuals, &neg, false},
	}
	for _, tt := range tests {
		fb, err := story.NewFeedback(tt.text, tt.scope, tt.at, uuid.Nil, now)
		if (err == nil) != tt.ok {
			t.Errorf("NewFeedback(%q, %q) = %v", tt.text, tt.scope, err)
		}
		if err == nil && fb.Scope != story.ScopeAll && tt.scope == "" {
			t.Error("empty scope not defaulted")
		}
	}
}
