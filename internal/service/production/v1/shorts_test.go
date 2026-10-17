package production_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	production "kurgu/internal/service/production/v1"
	"kurgu/pkg/archive"
	"kurgu/pkg/id"
)

func sec(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// timingsJSON is a narration of length seconds in 4.6 s sentences, each
// followed by a 0.4 s pause.
func timingsJSON(length float64) []byte {
	var segs []map[string]any
	for s := 0.0; s+4.6 <= length; s += 5 {
		segs = append(segs, map[string]any{"text": fmt.Sprintf("Cümle %d.", len(segs)+1), "start": s, "end": s + 4.6})
	}
	b, _ := json.Marshal(map[string]any{"engine": "okur", "duration": length, "words": []any{}, "segments": segs})
	return b
}

// longVideo stores a story whose long video went through review and its
// final render, measured at length, with a revision whose working
// files hold files.
func longVideo(t *testing.T, f fixture, length time.Duration, live bool, files map[string][]byte) (*storydomain.Story, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var stories storyrepo.Repository
	f.hostWith(func(d *production.Deps) { stories = d.Stories })

	dir := t.TempDir()
	for name, b := range files {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o750)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := archive.TarGz(dir, &buf); err != nil {
		t.Fatal(err)
	}
	key := "test/" + id.New().String() + "/workdir.tar.gz"
	if err := f.blobs.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Fatal(err)
	}

	spec, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatLong, pipelinedomain.Horizontal, 10*time.Minute)
	o, _ := pipelinedomain.NewOutputs(&spec, nil, "")
	idea, _ := storydomain.NewIdea("Duygu adlandırma", "açı", nil)
	st, err := storydomain.New(id.New(), storydomain.Plan{
		WorkspaceID: f.ws, Origin: storydomain.OriginManual, Language: pipelinedomain.Turkish, Idea: idea, Outputs: o,
	}, id.New, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := stories.Create(ctx, st); err != nil {
		t.Fatal(err)
	}
	lead := st.Lead()
	_, _ = st.StartDrafting(lead.ID(), time.Now())
	rev, err := st.Drafted(lead.ID(), map[string]string{storydomain.ArtifactWorkdir: key}, "s", "", storydomain.Measures{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := stories.PutRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	_ = st.Rendered(lead.ID(), length, time.Now())
	if live {
		_, _ = st.Approve(lead.ID(), id.New(), id.New, time.Now())
		if err := st.Rendered(lead.ID(), length, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := stories.Save(ctx, st); err != nil {
		t.Fatal(err)
	}
	return st, lead.ID()
}

func TestMomentsOfALongVideo(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	st, long := longVideo(t, f, 10*time.Minute, true, map[string][]byte{"narration/timings.json": timingsJSON(600)})

	view, err := f.svc.Moments(ctx, f.ws, st.ID(), long)
	if err != nil {
		t.Fatal(err)
	}
	if view.Blocked != "" || len(view.Sentences) != 120 || view.Sentences[1].Text != "Cümle 2." ||
		view.Sentences[1].Start != 5*time.Second || view.Sentences[1].End != sec(9.6) {
		t.Fatalf("view = blocked %q, %d sentences: %+v", view.Blocked, len(view.Sentences), view.Sentences[:2])
	}
	if view.Limits.MaxPerVideo() != storydomain.DefaultShortsLimits().MaxPerVideo() {
		t.Fatalf("limits = %+v", view.Limits)
	}

	c, _ := storydomain.NewClip(sec(12.3), sec(41))
	_, m, err := f.svc.AddMoment(ctx, f.ws, st.ID(), long, storydomain.MomentDraft{Clip: c, Hook: "Adını koy"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Clip().Start() != 10*time.Second || m.Clip().End() != sec(39.6) || m.Origin() != storydomain.MomentByPerson {
		t.Fatalf("moment = %v %s", m.Clip(), m.Origin())
	}

	moved, _ := storydomain.NewClip(sec(100), sec(131))
	hook := "Öfkeyi adlandır"
	if _, m, err = f.svc.AdjustMoment(ctx, f.ws, st.ID(), m.ID(), &moved, &hook); err != nil {
		t.Fatal(err)
	}
	if m.Clip().Start() != 100*time.Second || m.Clip().End() != sec(129.6) || m.Hook() != hook {
		t.Fatalf("adjusted = %v %q", m.Clip(), m.Hook())
	}

	got, err := f.svc.DiscardMoment(ctx, f.ws, st.ID(), m.ID())
	if err != nil {
		t.Fatal(err)
	}
	if ms := got.Moments(long); len(ms) != 1 || ms[0].Status() != storydomain.MomentDiscarded || ms[0].Hook() != hook {
		t.Fatalf("moments after discard = %+v", ms)
	}
	if _, _, err := f.svc.AdjustMoment(ctx, f.ws, st.ID(), m.ID(), nil, &hook); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("adjusting a discarded moment: %v", err)
	}
}

func TestMomentsNeedACuttableLongVideoWithTimings(t *testing.T) {
	ctx := context.Background()
	timings := map[string][]byte{"narration/timings.json": timingsJSON(600)}
	tests := []struct {
		name        string
		length      time.Duration
		live        bool
		files       map[string][]byte
		wantBlocked bool
		wantErr     apperror.Code // from AddMoment
	}{
		{"in review", 10 * time.Minute, false, timings, true, apperror.InvalidTransition},
		{"shorter than the source minimum", 3 * time.Minute, true, timings, true, apperror.ValidationFailed},
		{"no timings kept", 10 * time.Minute, true, map[string][]byte{"script.md": []byte("x")}, false, apperror.NotFound},
		{"timings without sentences", 10 * time.Minute, true, map[string][]byte{"narration/timings.json": []byte(`{"segments":[]}`)}, false, apperror.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			st, long := longVideo(t, f, tt.length, tt.live, tt.files)
			view, err := f.svc.Moments(ctx, f.ws, st.ID(), long)
			if tt.wantBlocked {
				if err != nil || view.Blocked == "" || view.Sentences != nil {
					t.Fatalf("view = blocked %q, %d sentences, %v", view.Blocked, len(view.Sentences), err)
				}
			} else if code, _ := apperror.GetCode(err); code != tt.wantErr {
				t.Fatalf("Moments err = %v, want %s", err, tt.wantErr)
			}
			c, _ := storydomain.NewClip(sec(10), sec(40))
			if _, _, err := f.svc.AddMoment(ctx, f.ws, st.ID(), long, storydomain.MomentDraft{Clip: c}); !apperror.Is(err, tt.wantErr) {
				t.Fatalf("AddMoment err = %v, want %s", err, tt.wantErr)
			}
		})
	}
}
