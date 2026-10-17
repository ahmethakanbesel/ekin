package story_test

import (
	"testing"
	"time"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	story "kurgu/internal/domain/story/v1"
	"kurgu/pkg/id"
)

func TestParseThumbnails(t *testing.T) {
	three := `{"recommended": 2, "variants": [
		{"n": 1, "concept": "frame", "text": "Kaygı  mı, endişe mi?", "still": 12.4},
		{"n": 2, "concept": "type", "text": "3 fark"},
		{"n": 3, "concept": "contrast", "text": "Kaygı ≠ endişe"}]}`
	tests := []struct {
		name string
		raw  string
		want int
		code apperror.Code
	}{
		{name: "the count asked for", raw: three, want: 3},
		{name: "any count up to four", raw: three, want: 0},
		{name: "a different count", raw: three, want: 2, code: apperror.ValidationFailed},
		{name: "not JSON", raw: `{`, code: apperror.ValidationFailed},
		{name: "none", raw: `{"recommended": 1, "variants": []}`, code: apperror.ValidationFailed},
		{name: "five", raw: `{"recommended": 1, "variants": [{"n":1,"concept":"a","text":"a"},{"n":2,"concept":"a","text":"a"},{"n":3,"concept":"a","text":"a"},{"n":4,"concept":"a","text":"a"},{"n":4,"concept":"a","text":"a"}]}`, code: apperror.ValidationFailed},
		{name: "repeated number", raw: `{"recommended": 1, "variants": [{"n":1,"concept":"a","text":"a"},{"n":1,"concept":"b","text":"b"}]}`, code: apperror.ValidationFailed},
		{name: "number out of range", raw: `{"recommended": 5, "variants": [{"n":5,"concept":"a","text":"a"}]}`, code: apperror.ValidationFailed},
		{name: "recommended is not a variant", raw: `{"recommended": 2, "variants": [{"n":1,"concept":"a","text":"a"}]}`, code: apperror.ValidationFailed},
		{name: "no concept", raw: `{"recommended": 1, "variants": [{"n":1,"concept":" ","text":"a"}]}`, code: apperror.ValidationFailed},
		{name: "no text", raw: `{"recommended": 1, "variants": [{"n":1,"concept":"a","text":""}]}`, code: apperror.ValidationFailed},
		{name: "six words", raw: `{"recommended": 1, "variants": [{"n":1,"concept":"a","text":"bir iki üç dört beş altı"}]}`, code: apperror.ValidationFailed},
		{name: "negative still", raw: `{"recommended": 1, "variants": [{"n":1,"concept":"a","text":"a","still":-1}]}`, code: apperror.ValidationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := story.ParseThumbnails([]byte(tt.raw), tt.want)
			if tt.code != "" {
				if !apperror.Is(err, tt.code) {
					t.Fatalf("ParseThumbnails() error = %v, want %s", err, tt.code)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Recommended != 2 || len(got.Variants) != 3 || got.Variants[0].Text != "Kaygı mı, endişe mi?" ||
				got.Variants[0].Still == nil || *got.Variants[0].Still != 12.4 || got.Variants[1].Still != nil {
				t.Fatalf("ParseThumbnails() = %+v", got)
			}
		})
	}
}

func TestThumbnailsWithText(t *testing.T) {
	th, _ := story.ParseThumbnails([]byte(`{"recommended": 1, "variants": [{"n":1,"concept":"a","text":"eski"},{"n":2,"concept":"b","text":"öteki"}]}`), 2)
	got, err := th.WithText(2, "  yeni   söz ")
	if err != nil || got.Variants[1].Text != "yeni söz" || got.Variants[0].Text != "eski" || th.Variants[1].Text != "öteki" {
		t.Fatalf("WithText() = %+v, %v; original %+v", got, err, th)
	}
	if _, err := th.WithText(3, "yok"); !apperror.Is(err, apperror.NotFound) {
		t.Fatalf("WithText(missing) = %v", err)
	}
	if _, err := th.WithText(1, "bir iki üç dört beş altı"); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("WithText(six words) = %v", err)
	}
}

func TestPickThumbnail(t *testing.T) {
	th, _ := story.ParseThumbnails([]byte(`{"recommended": 2, "variants": [{"n":1,"concept":"a","text":"a"},{"n":2,"concept":"b","text":"b"},{"n":3,"concept":"c","text":"c"}]}`), 3)
	s := newStory(t, outputs(t, true, false, pipelinedomain.KindVideo))
	v := s.Lead()
	if err := s.PickThumbnail(v.ID(), 1, th, now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("pick before review = %v", err)
	}
	_, _ = s.StartDrafting(v.ID(), now)
	_, _ = s.Drafted(v.ID(), nil, "s", "", story.Measures{}, now)
	_ = s.Rendered(v.ID(), time.Minute, now)

	if got := v.ThumbnailFor(th); got != 2 {
		t.Fatalf("ThumbnailFor() before a pick = %d, want the recommended 2", got)
	}
	if err := s.PickThumbnail(v.ID(), 3, th, now); err != nil || v.Thumbnail() != 3 || v.ThumbnailFor(th) != 3 {
		t.Fatalf("pick 3 = %v, picked %d", err, v.Thumbnail())
	}
	if err := s.PickThumbnail(v.ID(), 4, th, now); !apperror.Is(err, apperror.ValidationFailed) || v.Thumbnail() != 3 {
		t.Fatalf("pick a missing variant = %v, picked %d", err, v.Thumbnail())
	}
	if err := s.PickThumbnail(v.ID(), 1, story.Thumbnails{}, now); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("pick without variants = %v", err)
	}
	// A picked variant the revision no longer has falls back to the
	// recommended one; none at all to the poster frame.
	fewer, _ := story.ParseThumbnails([]byte(`{"recommended": 1, "variants": [{"n":1,"concept":"a","text":"a"}]}`), 1)
	if got := v.ThumbnailFor(fewer); got != 1 {
		t.Fatalf("ThumbnailFor(fewer) = %d", got)
	}
	if got := v.ThumbnailFor(story.Thumbnails{}); got != 0 {
		t.Fatalf("ThumbnailFor(none) = %d", got)
	}

	// The pick survives storage and a new revision resets it.
	back, err := story.Rehydrate(s.Snapshot())
	if err != nil || back.Lead().Thumbnail() != 3 {
		t.Fatalf("rehydrated pick = %v, %v", back.Lead().Thumbnail(), err)
	}
	fb, _ := story.NewFeedback([]story.Note{{Text: "Daha büyük yazı", Scope: story.ScopeThumbnail}}, id.New(), now)
	_ = s.RequestChanges(v.ID(), fb, now)
	if err := s.PickThumbnail(v.ID(), 1, th, now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("pick while queued = %v", err)
	}
	_, _ = s.StartDrafting(v.ID(), now)
	_, _ = s.Drafted(v.ID(), nil, "s", "", story.Measures{}, now)
	if v.Thumbnail() != 0 {
		t.Fatalf("pick after a new revision = %d", v.Thumbnail())
	}

	// Approved and finalizing or ready may still change the pick.
	_ = s.Rendered(v.ID(), time.Minute, now)
	_, _ = s.Approve(v.ID(), id.New(), id.New, now)
	if err := s.PickThumbnail(v.ID(), 1, th, now); err != nil {
		t.Fatalf("pick while finalizing = %v", err)
	}
	snap := s.Snapshot()
	snap.Deliverables[0].Thumbnail = 9
	if _, err := story.Rehydrate(snap); err == nil {
		t.Fatal("rehydrate accepted thumbnail 9")
	}
}

func TestThumbnailOnly(t *testing.T) {
	note := func(scope story.Scope) story.Note { return story.Note{Text: "x", Scope: scope} }
	tests := []struct {
		name  string
		notes []story.Note
		want  bool
	}{
		{"all about the thumbnails", []story.Note{note(story.ScopeThumbnail), note(story.ScopeThumbnail)}, true},
		{"one about the video", []story.Note{note(story.ScopeThumbnail), note(story.ScopeVisuals)}, false},
		{"the whole draft", []story.Note{note(story.ScopeAll)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb, err := story.NewFeedback(tt.notes, id.New(), now)
			if err != nil {
				t.Fatal(err)
			}
			if got := fb.ThumbnailOnly(); got != tt.want {
				t.Fatalf("ThumbnailOnly() = %v, want %v", got, tt.want)
			}
		})
	}
	var none *story.Feedback
	if none.ThumbnailOnly() {
		t.Fatal("nil feedback is thumbnail-only")
	}
}

func TestIsThumbnailArtifact(t *testing.T) {
	for name, want := range map[string]bool{
		"thumb-1.jpg": true, "thumb-4.jpg": true, "thumb-5.jpg": false, "thumb-0.jpg": false,
		"thumb-1.png": false, "poster.jpg": false, "../thumb-1.jpg": false,
	} {
		if got := story.IsThumbnailArtifact(name); got != want {
			t.Errorf("IsThumbnailArtifact(%q) = %v, want %v", name, got, want)
		}
	}
}
