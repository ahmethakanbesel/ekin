package httptransport_test

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

	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	"kurgu/internal/gen"
	"kurgu/pkg/archive"
	"kurgu/pkg/id"
)

// liveLongVideo stores a story in ws whose ten-minute long video is
// approved and rendered, with narration timings of 4.6 s sentences.
func (s *server) liveLongVideo(t *testing.T, ws uuid.UUID) (storyID, longID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var segs []map[string]any
	for x := 0.0; x+4.6 <= 600; x += 5 {
		segs = append(segs, map[string]any{"text": fmt.Sprintf("Cümle %.0f.", x/5+1), "start": x, "end": x + 4.6})
	}
	timings, _ := json.Marshal(map[string]any{"duration": 600, "words": []any{}, "segments": segs})
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "narration"), 0o750)
	_ = os.WriteFile(filepath.Join(dir, "narration", "timings.json"), timings, 0o600)
	var buf bytes.Buffer
	if err := archive.TarGz(dir, &buf); err != nil {
		t.Fatal(err)
	}
	key := "test/" + id.New().String() + "/workdir.tar.gz"
	if err := s.blobs.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Fatal(err)
	}

	spec, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatLong, pipelinedomain.Horizontal, 10*time.Minute)
	o, _ := pipelinedomain.NewOutputs(&spec, nil, "")
	idea, _ := storydomain.NewIdea("Duygu adlandırma", "açı", nil)
	st, err := storydomain.New(id.New(), storydomain.Plan{
		WorkspaceID: ws, Origin: storydomain.OriginManual, Language: pipelinedomain.Turkish, Idea: idea, Outputs: o,
	}, id.New, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.stories.Create(ctx, st); err != nil {
		t.Fatal(err)
	}
	long := st.Lead()
	_, _ = st.StartDrafting(long.ID(), time.Now())
	rev, _ := st.Drafted(long.ID(), map[string]string{storydomain.ArtifactWorkdir: key}, "s", "", storydomain.Measures{}, time.Now())
	if err := s.stories.PutRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	_ = st.Rendered(long.ID(), 10*time.Minute, time.Now())
	_, _ = st.Approve(long.ID(), id.New(), id.New, time.Now())
	if err := st.Rendered(long.ID(), 10*time.Minute, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.stories.Save(ctx, st); err != nil {
		t.Fatal(err)
	}
	return st.ID(), long.ID()
}

func TestShortsAPI(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	storyID, longID := srv.liveLongVideo(t, ws.Id)
	story := fmt.Sprintf("/api/v1/workspaces/%s/stories/%s", ws.Id, storyID)
	moments := fmt.Sprintf("%s/deliverables/%s/moments", story, longID)

	var view gen.Moments
	owner.call("GET", moments, nil, 200, &view)
	if view.Blocked != nil || len(view.Sentences) != 120 || len(view.Moments) != 0 || view.Shorts != 0 ||
		view.Limits.MaxPerVideo != 5 || view.Limits.SourceMinSeconds != 240 || view.Limits.ClipMaxSeconds != 60 {
		t.Fatalf("moments = %+v", view)
	}
	if s := view.Sentences[2]; s.Text != "Cümle 3." || s.StartSeconds != 10 || s.EndSeconds != 14.6 {
		t.Fatalf("sentence = %+v", s)
	}

	var scan gen.Job
	owner.call("POST", moments+"/scan", nil, 202, &scan)
	if scan.Kind != "story.moments" || scan.Status != gen.JobStatusQueued {
		t.Fatalf("scan job = %+v", scan)
	}

	var first, second gen.Moment
	owner.call("POST", moments, map[string]any{"startSeconds": 12.3, "endSeconds": 41, "hook": "Adını koy"}, 201, &first)
	if first.Clip.StartSeconds != 10 || first.Clip.EndSeconds != 39.6 || first.Origin != gen.MomentOriginManual ||
		first.Status != gen.MomentStatusSuggested || first.Hook != "Adını koy" || first.SourceId != longID {
		t.Fatalf("moment = %+v", first)
	}
	owner.call("POST", moments, map[string]any{"startSeconds": 300, "endSeconds": 330}, 201, &second)
	owner.fail("POST", moments, map[string]any{"startSeconds": 100, "endSeconds": 105}, 400, "ValidationFailed")

	moment := fmt.Sprintf("%s/moments/%s", story, first.Id)
	owner.fail("PATCH", moment, map[string]any{"startSeconds": 20}, 400, "ValidationFailed")
	owner.fail("PATCH", moment, map[string]any{}, 400, "ValidationFailed")
	var moved gen.Moment
	owner.call("PATCH", moment, map[string]any{"startSeconds": 50, "endSeconds": 80, "hook": "Öfkeyi adlandır"}, 200, &moved)
	if moved.Clip.StartSeconds != 50 || moved.Clip.EndSeconds != 79.6 || moved.Hook != "Öfkeyi adlandır" {
		t.Fatalf("moved = %+v", moved)
	}
	owner.call("DELETE", fmt.Sprintf("%s/moments/%s", story, second.Id), nil, 204, nil)

	var st gen.Story
	owner.call("POST", story+"/deliverables/"+longID.String()+"/shorts", map[string]any{"momentIds": []uuid.UUID{first.Id}}, 200, &st)
	var short *gen.Deliverable
	for i, d := range st.Deliverables {
		if d.Clip != nil {
			short = &st.Deliverables[i]
		}
	}
	if short == nil || short.Clip.StartSeconds != 50 || short.Clip.EndSeconds != 79.6 || short.SourceId == nil || *short.SourceId != longID ||
		short.Status != gen.DeliverableStatusQueued || short.Video == nil || short.Video.Format != gen.VideoSpecFormatShort {
		t.Fatalf("short = %+v", short)
	}
	owner.fail("POST", story+"/deliverables/"+longID.String()+"/shorts", map[string]any{"momentIds": []uuid.UUID{second.Id}}, 409, "InvalidTransition")
	owner.fail("POST", story+"/deliverables/"+longID.String()+"/shorts", map[string]any{"momentIds": []uuid.UUID{}}, 400, "ValidationFailed")

	owner.call("GET", moments, nil, 200, &view)
	if view.Shorts != 1 || len(view.Moments) != 2 || view.Moments[0].Status != gen.MomentStatusMade ||
		view.Moments[0].ShortId == nil || *view.Moments[0].ShortId != short.Id || view.Moments[1].Status != gen.MomentStatusDiscarded {
		t.Fatalf("moments after making a short = %+v", view.Moments)
	}

	// A short is not a long video: nothing can be cut from it.
	owner.call("GET", fmt.Sprintf("%s/deliverables/%s/moments", story, short.Id), nil, 200, &view)
	if view.Blocked == nil || len(view.Sentences) != 0 {
		t.Fatalf("moments of a short = %+v", view)
	}
	owner.fail("GET", fmt.Sprintf("%s/deliverables/%s/moments", story, id.New()), nil, 404, "NotFound")
	owner.fail("POST", fmt.Sprintf("%s/deliverables/%s/moments/scan", story, short.Id), nil, 400, "ValidationFailed")

	// Another workspace's member sees nothing.
	stranger := srv.signedIn(t, "stranger@example.com")
	stranger.workspace("other")
	stranger.fail("GET", moments, nil, 404, "WorkspaceNotFound")
}
