package handler

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	"kurgu/internal/gen"
	notifysvc "kurgu/internal/service/notify/v1"
	"kurgu/pkg/id"
)

func TestFeedbackToWireNamesTheAuthor(t *testing.T) {
	author := uuid.New()
	at := 21.5
	tests := []struct {
		name string
		by   uuid.UUID
		want *uuid.UUID
	}{
		{name: "written by a person", by: author, want: &author},
		{name: "no author stored", by: uuid.Nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb, err := storydomain.NewFeedback([]storydomain.Note{{Text: "Slow the voice down", Scope: storydomain.ScopeVoice, AtSeconds: &at}}, tt.by, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			got := feedbackToWire(&fb)
			switch {
			case tt.want == nil && got.By != nil:
				t.Fatalf("by = %v, want none", *got.By)
			case tt.want != nil && (got.By == nil || *got.By != *tt.want):
				t.Fatalf("by = %v, want %v", got.By, *tt.want)
			}
		})
	}
}

func TestMeasuresToWireLeavesOutWhatWasNotMeasured(t *testing.T) {
	if got := measuresToWire(storydomain.Measures{}); got != nil {
		t.Fatalf("unmeasured = %+v, want nil", got)
	}
	words := measuresToWire(storydomain.Measures{Words: 140})
	if words == nil || words.DurationSeconds != nil || words.Words == nil || *words.Words != 140 {
		t.Fatalf("words only = %+v", words)
	}
	both := measuresToWire(storydomain.Measures{Duration: 47520 * time.Millisecond, Words: 98})
	if both == nil || both.DurationSeconds == nil || *both.DurationSeconds != 47.52 || *both.Words != 98 {
		t.Fatalf("both = %+v", both)
	}
	if both.Narrator != nil {
		t.Fatalf("no narrator = %+v", both.Narrator)
	}
	local := measuresToWire(storydomain.Measures{Narrator: storydomain.Narrator{Engine: "ema"}})
	if local == nil || local.Narrator == nil || local.Narrator.Engine != "ema" || local.Narrator.Voice != nil || local.Narrator.Model != nil {
		t.Fatalf("local voice = %+v", local)
	}
	cloud := measuresToWire(storydomain.Measures{Narrator: storydomain.Narrator{Engine: "elevenlabs", Voice: "v1", Model: "eleven_v3"}})
	if n := cloud.Narrator; n == nil || *n.Voice != "v1" || *n.Model != "eleven_v3" {
		t.Fatalf("cloud voice = %+v", n)
	}
}

func TestFeedbackToWireKeepsEveryNote(t *testing.T) {
	at := 14.2
	fb, err := storydomain.NewFeedback([]storydomain.Note{
		{Text: "Wrong chart", Scope: storydomain.ScopeVisuals, AtSeconds: &at},
		{Text: "Tighter intro"},
	}, uuid.Nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := feedbackToWire(&fb)
	tests := []struct {
		text  string
		scope string
		at    *float64
	}{
		{"Tighter intro", "all", nil},
		{"Wrong chart", "visuals", &at},
	}
	if len(got.Notes) != len(tests) {
		t.Fatalf("notes = %+v", got.Notes)
	}
	for i, tt := range tests {
		n := got.Notes[i]
		if n.Text != tt.text || string(n.Scope) != tt.scope || (n.AtSeconds == nil) != (tt.at == nil) ||
			(tt.at != nil && *n.AtSeconds != *tt.at) {
			t.Errorf("note %d = %+v, want %+v", i, n, tt)
		}
	}
}

// fakeFeed records the filter it was asked for.
type fakeFeed struct {
	got  notifysvc.FeedFilter
	next string
}

func (f *fakeFeed) Feed(_ context.Context, _ uuid.UUID, filter notifysvc.FeedFilter) ([]notifysvc.Notification, string, error) {
	f.got = filter
	return nil, f.next, nil
}

func TestListNotificationsPassesItsFilter(t *testing.T) {
	kinds := func(k ...gen.ListNotificationsParamsKind) *[]gen.ListNotificationsParamsKind { return &k }
	tests := []struct {
		name    string
		params  gen.ListNotificationsParams
		next    string
		want    notifysvc.FeedFilter
		wantErr apperror.Code
	}{
		{name: "defaults", want: notifysvc.FeedFilter{}},
		{name: "kinds, limit and cursor", params: gen.ListNotificationsParams{
			Kind: kinds(gen.ListNotificationsParamsKindFailed, gen.ListNotificationsParamsKindReview), Limit: ptr(10), Cursor: ptr("c1"),
		}, next: "c2", want: notifysvc.FeedFilter{Kinds: []string{"failed", "review"}, Limit: 10, After: "c1"}},
		{name: "unknown kind", params: gen.ListNotificationsParams{Kind: kinds("lost")}, wantErr: apperror.ValidationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feed := &fakeFeed{next: tt.next}
			res, err := New(nil, feed).ListNotifications(context.Background(), gen.ListNotificationsRequestObject{
				WorkspaceId: uuid.New(), Params: tt.params,
			})
			if tt.wantErr != "" {
				if !apperror.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(feed.got, tt.want) {
				t.Fatalf("filter = %+v, want %+v", feed.got, tt.want)
			}
			out := res.(gen.ListNotifications200JSONResponse)
			if (out.NextCursor == nil) != (tt.next == "") || out.NextCursor != nil && *out.NextCursor != tt.next {
				t.Fatalf("next = %v, want %q", out.NextCursor, tt.next)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// A video waiting for its script's approval says so, and stops saying so
// once the script is approved.
func TestToWireShowsTheScriptStage(t *testing.T) {
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	v, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 45*time.Second)
	o, _ := pipelinedomain.NewOutputs(&v, nil, "")
	idea, _ := storydomain.NewIdea("Kaygı", "açı", nil)
	st, err := storydomain.New(id.New(), storydomain.Plan{
		WorkspaceID: id.New(), Origin: storydomain.OriginAdHoc, Language: pipelinedomain.Turkish, Idea: idea, Outputs: o,
		Brief: storydomain.Brief{Production: pipelinedomain.Production{ReviewAt: pipelinedomain.ReviewScript}},
	}, id.New, now)
	if err != nil {
		t.Fatal(err)
	}
	wire := ToWire(st)
	if s := wire.Deliverables[0].Stage; s == nil || *s != gen.DeliverableStageScript {
		t.Fatalf("stage = %v", s)
	}
	if p := wire.Production; p == nil || p.ReviewAt == nil || *p.ReviewAt != gen.ProductionReviewAtScript {
		t.Fatalf("production = %+v", p)
	}
	d := st.Lead()
	_, _ = st.StartDrafting(d.ID(), now)
	_, _ = st.Drafted(d.ID(), nil, "s", "", storydomain.Measures{}, now)
	if _, err := st.Approve(d.ID(), id.New(), id.New, now); err != nil {
		t.Fatal(err)
	}
	wire = ToWire(st)
	if s := wire.Deliverables[0].Stage; s == nil || *s != gen.DeliverableStageVideo || wire.Deliverables[0].ScriptApprovedAt == nil {
		t.Fatalf("after approval: stage %v, script approved %v", s, wire.Deliverables[0].ScriptApprovedAt)
	}
}
