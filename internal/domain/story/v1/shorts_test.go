package story_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	pipelinedomain "ekin/internal/domain/pipeline/v1"
	story "ekin/internal/domain/story/v1"
	"ekin/pkg/id"
)

func sec(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

func clip(t *testing.T, start, end float64) story.Clip {
	t.Helper()
	c, err := story.NewClip(sec(start), sec(end))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// sentences are a narration of length seconds in 4.6 s sentences, each
// followed by a 0.4 s pause: 0–4.6, 5–9.6, 10–14.6, …
func sentences(length float64) []story.Segment {
	var out []story.Segment
	for s := 0.0; s+4.6 <= length; s += 5 {
		out = append(out, story.Segment{Start: sec(s), End: sec(s + 4.6)})
	}
	return out
}

func codeOf(err error) apperror.Code {
	code, _ := apperror.GetCode(err)
	return code
}

// longStory returns a story whose long video lead went through review
// and its final render, measured at length; brief overrides the brief.
func longStory(t *testing.T, length time.Duration, brief story.Brief) (*story.Story, *story.Deliverable) {
	t.Helper()
	spec, err := pipelinedomain.NewVideoSpec(pipelinedomain.FormatLong, pipelinedomain.Horizontal, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	o, err := pipelinedomain.NewOutputs(&spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	idea, _ := story.NewIdea("Duygu adlandırma", "", nil)
	s, err := story.New(id.New(), story.Plan{
		WorkspaceID: id.New(), Origin: story.OriginSchedule, Language: pipelinedomain.Turkish,
		Idea: idea, Brief: brief, Outputs: o,
	}, id.New, now)
	if err != nil {
		t.Fatal(err)
	}
	lead := s.Lead()
	if s.AtScript(lead.ID()) {
		_, _ = s.StartDrafting(lead.ID(), now)
		_, _ = s.Drafted(lead.ID(), nil, "", "", story.Measures{}, now)
		if _, err := s.Approve(lead.ID(), id.New(), id.New, now); err != nil {
			t.Fatalf("approve script: %v", err)
		}
	}
	if _, err := s.StartDrafting(lead.ID(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Drafted(lead.ID(), nil, "", "", story.Measures{}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Rendered(lead.ID(), length, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(lead.ID(), id.New(), id.New, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Rendered(lead.ID(), length, now); err != nil || lead.Status() != story.StatusReady {
		t.Fatalf("final render = %s %v", lead.Status(), err)
	}
	return s, lead
}

func TestNewShortsLimits(t *testing.T) {
	def := story.DefaultShortsLimits()
	if _, err := story.NewShortsLimits(def.SourceMin(), def.SourceMax(), def.ClipMin(), def.ClipMax(), def.MaxPerVideo(), def.MinGap()); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
	tests := []struct {
		name             string
		srcMin, srcMax   time.Duration
		clipMin, clipMax time.Duration
		perVideo         int
		gap              time.Duration
		wantErr          bool
	}{
		{"defaults", 4 * time.Minute, time.Hour, 20 * time.Second, time.Minute, 5, 30 * time.Second, false},
		{"shorts off", 4 * time.Minute, time.Hour, 20 * time.Second, time.Minute, 0, 0, false},
		{"youtube bounds", time.Minute, time.Hour, 10 * time.Second, 3 * time.Minute, 20, 0, false},
		{"zero source minimum", 0, time.Hour, 20 * time.Second, time.Minute, 5, 0, true},
		{"source minimum above maximum", time.Hour, 4 * time.Minute, 20 * time.Second, time.Minute, 5, 0, true},
		{"short under 10 seconds", 4 * time.Minute, time.Hour, 9 * time.Second, time.Minute, 5, 0, true},
		{"short over 3 minutes", 4 * time.Minute, time.Hour, 20 * time.Second, 3*time.Minute + time.Second, 5, 0, true},
		{"short minimum above maximum", 4 * time.Minute, time.Hour, 50 * time.Second, 40 * time.Second, 5, 0, true},
		{"short minimum longer than the shortest source", 30 * time.Second, time.Hour, 40 * time.Second, time.Minute, 5, 0, true},
		{"too many per video", 4 * time.Minute, time.Hour, 20 * time.Second, time.Minute, story.MaxShortsPerVideo + 1, 0, true},
		{"negative per video", 4 * time.Minute, time.Hour, 20 * time.Second, time.Minute, -1, 0, true},
		{"negative gap", 4 * time.Minute, time.Hour, 20 * time.Second, time.Minute, 5, -time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := story.NewShortsLimits(tt.srcMin, tt.srcMax, tt.clipMin, tt.clipMax, tt.perVideo, tt.gap)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

func TestCanCut(t *testing.T) {
	limits := story.DefaultShortsLimits()
	tests := []struct {
		name    string
		make    func(t *testing.T) (*story.Story, uuid.UUID)
		wantErr apperror.Code
	}{
		{"live long video", func(t *testing.T) (*story.Story, uuid.UUID) {
			s, lead := longStory(t, 10*time.Minute, story.Brief{})
			return s, lead.ID()
		}, ""},
		{"shorter than the source minimum", func(t *testing.T) (*story.Story, uuid.UUID) {
			s, lead := longStory(t, 3*time.Minute+59*time.Second, story.Brief{})
			return s, lead.ID()
		}, apperror.ValidationFailed},
		{"longer than the source maximum", func(t *testing.T) (*story.Story, uuid.UUID) {
			s, lead := longStory(t, time.Hour+time.Second, story.Brief{})
			return s, lead.ID()
		}, apperror.ValidationFailed},
		{"not measured", func(t *testing.T) (*story.Story, uuid.UUID) {
			s, lead := longStory(t, 0, story.Brief{})
			return s, lead.ID()
		}, apperror.InvalidTransition},
		{"in review", func(t *testing.T) (*story.Story, uuid.UUID) {
			spec, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatLong, pipelinedomain.Horizontal, 10*time.Minute)
			o, _ := pipelinedomain.NewOutputs(&spec, nil, "")
			s := newStory(t, o)
			lead := s.Lead()
			_, _ = s.StartDrafting(lead.ID(), now)
			_, _ = s.Drafted(lead.ID(), nil, "", "", story.Measures{}, now)
			_ = s.Rendered(lead.ID(), 10*time.Minute, now)
			return s, lead.ID()
		}, apperror.InvalidTransition},
		{"a short", func(t *testing.T) (*story.Story, uuid.UUID) {
			s := newStory(t, outputs(t, true, false, ""))
			return s, s.Lead().ID()
		}, apperror.ValidationFailed},
		{"an article", func(t *testing.T) (*story.Story, uuid.UUID) {
			s, article := readyArticle(t)
			return s, article.ID()
		}, apperror.ValidationFailed},
		{"unknown deliverable", func(t *testing.T) (*story.Story, uuid.UUID) {
			s, _ := longStory(t, 10*time.Minute, story.Brief{})
			return s, id.New()
		}, apperror.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, source := tt.make(t)
			_, err := s.CanCut(source, limits)
			if got := codeOf(err); got != tt.wantErr {
				t.Fatalf("err = %v, want code %q", err, tt.wantErr)
			}
		})
	}
}

func TestSuggestMomentsSnapsFitsAndSpaces(t *testing.T) {
	limits := story.DefaultShortsLimits() // 20–60 s, 30 s apart
	segs := sentences(600)
	tests := []struct {
		name   string
		drafts []story.MomentDraft
		want   [][2]float64 // kept clips, best first
	}{
		{
			name:   "snaps to the nearest sentence start and end",
			drafts: []story.MomentDraft{{Clip: clip(t, 12.3, 41), Hook: "Adını koy", Score: 80}},
			want:   [][2]float64{{10, 39.6}},
		},
		{
			name:   "ends a long moment at the last sentence that fits",
			drafts: []story.MomentDraft{{Clip: clip(t, 100, 200), Score: 50}},
			want:   [][2]float64{{100, 159.6}},
		},
		{
			name:   "drops a moment too short after snapping",
			drafts: []story.MomentDraft{{Clip: clip(t, 100, 114), Score: 90}},
		},
		{
			name: "keeps the better of two that overlap or sit too close",
			drafts: []story.MomentDraft{
				{Clip: clip(t, 100, 140), Score: 40},
				{Clip: clip(t, 120, 160), Score: 70},
				{Clip: clip(t, 180, 220), Score: 60}, // 20 s after the 70: too close
				{Clip: clip(t, 190, 230), Score: 30}, // 30 s after the 70: far enough
			},
			want: [][2]float64{{120, 159.6}, {190, 229.6}},
		},
		{
			name: "drops a moment with a bad score",
			drafts: []story.MomentDraft{
				{Clip: clip(t, 100, 140), Score: 101},
				{Clip: clip(t, 300, 340), Score: 10},
			},
			want: [][2]float64{{300, 339.6}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, lead := longStory(t, 10*time.Minute, story.Brief{})
			kept, err := s.SuggestMoments(lead.ID(), tt.drafts, segs, limits, id.New, now)
			if err != nil {
				t.Fatal(err)
			}
			var got [][2]float64
			for _, m := range kept {
				got = append(got, [2]float64{m.Clip().Start().Seconds(), m.Clip().End().Seconds()})
				if m.Origin() != story.MomentByAgent || m.Status() != story.MomentSuggested || m.SourceID() != lead.ID() {
					t.Fatalf("moment = %+v", m)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("kept %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSuggestMomentsReplacesOnlyTheAgentsSuggestions(t *testing.T) {
	limits := story.DefaultShortsLimits()
	segs := sentences(600)
	s, lead := longStory(t, 10*time.Minute, story.Brief{})
	first, err := s.SuggestMoments(lead.ID(), []story.MomentDraft{
		{Clip: clip(t, 0, 30), Score: 50},
		{Clip: clip(t, 100, 130), Score: 50},
		{Clip: clip(t, 200, 230), Score: 50},
	}, segs, limits, id.New, now)
	if err != nil || len(first) != 3 {
		t.Fatalf("first scan = %d %v", len(first), err)
	}
	if err := s.DiscardMoment(first[1].ID(), now); err != nil {
		t.Fatal(err)
	}
	hook := "Benim seçtiğim"
	if _, err := s.AdjustMoment(first[2].ID(), nil, &hook, segs, limits, now); err != nil {
		t.Fatal(err)
	}
	manual, err := s.AddMoment(lead.ID(), story.MomentDraft{Clip: clip(t, 400, 430)}, segs, limits, id.New, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SuggestMoments(lead.ID(), []story.MomentDraft{{Clip: clip(t, 500, 530), Score: 90}}, segs, limits, id.New, now); err != nil {
		t.Fatal(err)
	}
	var starts []float64
	for _, m := range s.Moments(lead.ID()) {
		starts = append(starts, m.Clip().Start().Seconds())
	}
	// 0: replaced; 100: discarded, kept as set aside; 200: adjusted by a
	// person; 400: added by a person; 500: the new suggestion.
	if want := []float64{100, 200, 400, 500}; !slices.Equal(starts, want) {
		t.Fatalf("moments start at %v, want %v", starts, want)
	}
	if manual.Origin() != story.MomentByPerson || manual.Score() != 0 {
		t.Fatalf("manual moment = %+v", manual)
	}
}

func TestAddShorts(t *testing.T) {
	limits, err := story.NewShortsLimits(4*time.Minute, time.Hour, 20*time.Second, time.Minute, 2, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	segs := sentences(600)
	type setup struct {
		s      *story.Story
		lead   *story.Deliverable
		moment func(start float64) uuid.UUID // adds a manual moment
	}
	prepare := func(t *testing.T) setup {
		s, lead := longStory(t, 10*time.Minute, story.Brief{})
		return setup{s: s, lead: lead, moment: func(start float64) uuid.UUID {
			m, err := s.AddMoment(lead.ID(), story.MomentDraft{Clip: clip(t, start, start+30)}, segs, limits, id.New, now)
			if err != nil {
				t.Fatal(err)
			}
			return m.ID()
		}}
	}
	tests := []struct {
		name    string
		pick    func(t *testing.T, u setup) []uuid.UUID
		wantErr apperror.Code
	}{
		{"two apart", func(_ *testing.T, u setup) []uuid.UUID { return []uuid.UUID{u.moment(0), u.moment(100)} }, ""},
		{"none", func(_ *testing.T, _ setup) []uuid.UUID { return nil }, apperror.ValidationFailed},
		{"more than the limit", func(_ *testing.T, u setup) []uuid.UUID {
			return []uuid.UUID{u.moment(0), u.moment(100), u.moment(200)}
		}, apperror.ValidationFailed},
		{"past the limit with shorts already made", func(t *testing.T, u setup) []uuid.UUID {
			if _, err := u.s.AddShorts(u.lead.ID(), []uuid.UUID{u.moment(0), u.moment(100)}, limits, id.New, now); err != nil {
				t.Fatal(err)
			}
			return []uuid.UUID{u.moment(200)}
		}, apperror.ValidationFailed},
		{"too close to each other", func(_ *testing.T, u setup) []uuid.UUID {
			return []uuid.UUID{u.moment(0), u.moment(50)}
		}, apperror.ValidationFailed},
		{"too close to one already made", func(t *testing.T, u setup) []uuid.UUID {
			if _, err := u.s.AddShorts(u.lead.ID(), []uuid.UUID{u.moment(0)}, limits, id.New, now); err != nil {
				t.Fatal(err)
			}
			return []uuid.UUID{u.moment(45)}
		}, apperror.ValidationFailed},
		{"chosen twice", func(_ *testing.T, u setup) []uuid.UUID {
			m := u.moment(0)
			return []uuid.UUID{m, m}
		}, apperror.ValidationFailed},
		{"discarded", func(_ *testing.T, u setup) []uuid.UUID {
			m := u.moment(0)
			_ = u.s.DiscardMoment(m, now)
			return []uuid.UUID{m}
		}, apperror.InvalidTransition},
		{"unknown", func(_ *testing.T, _ setup) []uuid.UUID { return []uuid.UUID{id.New()} }, apperror.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := prepare(t)
			picked := tt.pick(t, u)
			before := len(u.s.Deliverables())
			made, err := u.s.AddShorts(u.lead.ID(), picked, limits, id.New, now)
			if got := codeOf(err); got != tt.wantErr {
				t.Fatalf("err = %v, want code %q", err, tt.wantErr)
			}
			if err != nil {
				if len(u.s.Deliverables()) != before {
					t.Fatal("a refused AddShorts added deliverables")
				}
				return
			}
			for i, d := range made {
				m, _ := u.s.Moment(picked[i])
				if d.Role() != story.RoleDerived || d.SourceID() != u.lead.ID() || d.Status() != story.StatusQueued ||
					d.Video().Format() != pipelinedomain.FormatShort || d.Video().Orientation() != pipelinedomain.Vertical {
					t.Fatalf("short = %+v", d)
				}
				if d.Clip() == nil || *d.Clip() != m.Clip() || d.Video().Duration() != 30*time.Second {
					t.Fatalf("short clip %v duration %s, moment %v", d.Clip(), d.Video().Duration(), m.Clip())
				}
				if m.Status() != story.MomentMade || m.ShortID() != d.ID() {
					t.Fatalf("moment = %s → %s", m.Status(), m.ShortID())
				}
			}
			if got := len(u.s.Shorts(u.lead.ID())); got != len(made) {
				t.Fatalf("Shorts = %d, want %d", got, len(made))
			}
		})
	}
}

func TestShortsSurviveASnapshotAndSkipScriptReview(t *testing.T) {
	limits := story.DefaultShortsLimits()
	segs := sentences(600)
	s, lead := longStory(t, 10*time.Minute, story.Brief{Production: pipelinedomain.Production{ReviewAt: pipelinedomain.ReviewScript}})
	kept, err := s.SuggestMoments(lead.ID(), []story.MomentDraft{
		{Clip: clip(t, 10, 40), Hook: "Öfkeyi adlandır", Reason: "Tek başına anlaşılıyor", Score: 88},
		{Clip: clip(t, 300, 330), Score: 20},
	}, segs, limits, id.New, now)
	if err != nil || len(kept) != 2 {
		t.Fatalf("suggest = %d %v", len(kept), err)
	}
	made, err := s.AddShorts(lead.ID(), []uuid.UUID{kept[0].ID()}, limits, id.New, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.AtScript(made[0].ID()) {
		t.Fatal("a short cut from a long video waits for script review")
	}

	back, err := story.Rehydrate(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	short, err := back.Deliverable(made[0].ID())
	if err != nil || short.Clip() == nil || *short.Clip() != kept[0].Clip() {
		t.Fatalf("short after rehydrate = %+v %v", short, err)
	}
	moments := back.Moments(lead.ID())
	if len(moments) != 2 {
		t.Fatalf("moments after rehydrate = %d", len(moments))
	}
	m := moments[0]
	if m.Hook() != "Öfkeyi adlandır" || m.Reason() != "Tek başına anlaşılıyor" || m.Score() != 88 ||
		m.Status() != story.MomentMade || m.ShortID() != short.ID() || m.Clip() != kept[0].Clip() {
		t.Fatalf("moment after rehydrate = %+v", m)
	}
	if lead.Clip() != nil {
		t.Fatal("the long video has a clip")
	}
}

func TestRehydrateRejectsBadMoments(t *testing.T) {
	limits := story.DefaultShortsLimits()
	s, lead := longStory(t, 10*time.Minute, story.Brief{})
	if _, err := s.SuggestMoments(lead.ID(), []story.MomentDraft{{Clip: clip(t, 10, 40), Score: 5}}, sentences(600), limits, id.New, now); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*story.Snapshot)
	}{
		{"unknown source", func(sn *story.Snapshot) { sn.Moments[0].SourceID = id.New() }},
		{"unknown status", func(sn *story.Snapshot) { sn.Moments[0].Status = "kept" }},
		{"made without a short", func(sn *story.Snapshot) { sn.Moments[0].Status = string(story.MomentMade) }},
		{"empty clip", func(sn *story.Snapshot) { sn.Moments[0].Clip.End = sn.Moments[0].Clip.Start }},
		{"clip on the long video", func(sn *story.Snapshot) {
			sn.Deliverables[0].Clip = &story.ClipSnapshot{Start: 0, End: 30 * time.Second}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sn := s.Snapshot()
			sn.Moments = slices.Clone(sn.Moments)
			sn.Deliverables = slices.Clone(sn.Deliverables)
			tt.mutate(&sn)
			if _, err := story.Rehydrate(sn); err == nil {
				t.Fatal("rehydrated a broken snapshot")
			}
		})
	}
}

func TestRecut(t *testing.T) {
	limits := story.DefaultShortsLimits() // 20–60 s, 30 s apart
	segs := sentences(600)
	tests := []struct {
		name     string
		clip     [2]float64
		drafting bool
		wantErr  apperror.Code
	}{
		{"a sentence earlier", [2]float64{95, 129.6}, true, ""},
		{"too short", [2]float64{100, 114.6}, true, apperror.ValidationFailed},
		{"too long", [2]float64{100, 200}, true, apperror.ValidationFailed},
		{"too close to the other short", [2]float64{180, 219.6}, true, apperror.ValidationFailed},
		{"not drafting", [2]float64{95, 129.6}, false, apperror.InvalidTransition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, lead := longStory(t, 10*time.Minute, story.Brief{})
			kept, err := s.SuggestMoments(lead.ID(), []story.MomentDraft{
				{Clip: clip(t, 100, 130), Score: 90},
				{Clip: clip(t, 240, 270), Score: 80},
			}, segs, limits, id.New, now)
			if err != nil || len(kept) != 2 {
				t.Fatalf("suggest = %d %v", len(kept), err)
			}
			made, err := s.AddShorts(lead.ID(), []uuid.UUID{kept[0].ID(), kept[1].ID()}, limits, id.New, now)
			if err != nil {
				t.Fatal(err)
			}
			short := made[0]
			if tt.drafting {
				if _, err := s.StartDrafting(short.ID(), now); err != nil {
					t.Fatal(err)
				}
			}
			c := clip(t, tt.clip[0], tt.clip[1])
			err = s.Recut(short.ID(), c, limits, now)
			if got := codeOf(err); got != tt.wantErr {
				t.Fatalf("err = %v, want code %q", err, tt.wantErr)
			}
			m, _ := s.Moment(kept[0].ID())
			if err != nil {
				if *short.Clip() != kept[0].Clip() || m.Clip() != kept[0].Clip() {
					t.Fatal("a refused recut moved the clip")
				}
				return
			}
			if *short.Clip() != c || m.Clip() != c || short.Video().Duration() != 35*time.Second {
				t.Fatalf("after recut: short %v (%s), moment %v", short.Clip(), short.Video().Duration(), m.Clip())
			}
		})
	}
}
