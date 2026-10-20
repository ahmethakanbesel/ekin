package httptransport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	"github.com/ahmethakanbesel/ekin/pkg/archive"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// narratedVideo stores a story in ws whose video has one revision with
// narration (a narration.wav artifact and word timings in its working
// files) when narrated, or a script alone when not.
func (s *server) narratedVideo(t *testing.T, ws uuid.UUID, narrated bool) (storyID, videoID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	timings, _ := json.Marshal(map[string]any{"duration": 2, "words": []map[string]any{
		{"text": "Merhaba", "start": 0, "end": 0.4},
		{"text": "dünya.", "start": 0.5, "end": 0.9},
		{"text": "<Nasılsın?>", "start": 1.2, "end": 1.8},
	}})
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "narration"), 0o750)
	_ = os.WriteFile(filepath.Join(dir, "narration", "timings.json"), timings, 0o600)
	var buf bytes.Buffer
	if err := archive.TarGz(dir, &buf); err != nil {
		t.Fatal(err)
	}
	prefix := "test/" + id.New().String() + "/"
	artifacts := map[string]string{storydomain.ArtifactWorkdir: prefix + "workdir.tar.gz"}
	if err := s.blobs.Put(ctx, prefix+"workdir.tar.gz", bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Fatal(err)
	}
	if narrated {
		artifacts[storydomain.ArtifactNarration] = prefix + "narration.wav"
		if err := s.blobs.Put(ctx, prefix+"narration.wav", bytes.NewReader([]byte("RIFF")), 4); err != nil {
			t.Fatal(err)
		}
	}

	spec, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, time.Minute)
	o, _ := pipelinedomain.NewOutputs(&spec, nil, "")
	idea, _ := storydomain.NewIdea("Selam", "açı", nil)
	st, err := storydomain.New(id.New(), storydomain.Plan{
		WorkspaceID: ws, Origin: storydomain.OriginManual, Language: pipelinedomain.Turkish, Idea: idea, Outputs: o,
	}, id.New, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.stories.Create(ctx, st); err != nil {
		t.Fatal(err)
	}
	video := st.Lead()
	_, _ = st.StartDrafting(video.ID(), time.Now())
	rev, _ := st.Drafted(video.ID(), artifacts, "s", "", storydomain.Measures{}, time.Now())
	if err := s.stories.PutRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	if err := s.stories.Save(ctx, st); err != nil {
		t.Fatal(err)
	}
	return st.ID(), video.ID()
}

func TestCaptionsAPI(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	storyID, videoID := srv.narratedVideo(t, ws.Id, true)
	deliverable := fmt.Sprintf("/api/v1/workspaces/%s/stories/%s/deliverables/%s", ws.Id, storyID, videoID)

	var revs struct {
		Revisions []gen.Revision `json:"revisions"`
	}
	owner.call("GET", deliverable+"/revisions", nil, 200, &revs)
	if len(revs.Revisions) != 1 || !slices.Contains(revs.Revisions[0].Artifacts, "captions.srt") || !slices.Contains(revs.Revisions[0].Artifacts, "captions.vtt") {
		t.Fatalf("revisions = %+v", revs.Revisions)
	}

	tests := []struct {
		name, contentType, want string
	}{
		{"captions.srt", "application/x-subrip; charset=utf-8",
			"1\n00:00:00,000 --> 00:00:01,200\nMerhaba dünya.\n\n2\n00:00:01,200 --> 00:00:02,300\n<Nasılsın?>\n\n"},
		{"captions.vtt", "text/vtt; charset=utf-8",
			"WEBVTT\n\n00:00:00.000 --> 00:00:01.200\nMerhaba dünya.\n\n00:00:01.200 --> 00:00:02.300\n&lt;Nasılsın?&gt;\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := owner.do("GET", deliverable+"/revisions/1/"+tt.name+"?download=1", nil, nil)
			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if res.StatusCode != 200 || string(body) != tt.want {
				t.Fatalf("GET %s = %d %q, want %q", tt.name, res.StatusCode, body, tt.want)
			}
			if got := res.Header.Get("Content-Type"); got != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.contentType)
			}
			if got := res.Header.Get("Content-Disposition"); got != `attachment; filename="`+tt.name+`"` {
				t.Errorf("Content-Disposition = %q", got)
			}
		})
	}

	// A revision without narration has no captions to list or serve.
	storyID, videoID = srv.narratedVideo(t, ws.Id, false)
	silent := fmt.Sprintf("/api/v1/workspaces/%s/stories/%s/deliverables/%s", ws.Id, storyID, videoID)
	owner.call("GET", silent+"/revisions", nil, 200, &revs)
	if slices.Contains(revs.Revisions[0].Artifacts, "captions.srt") {
		t.Fatalf("artifacts without narration = %v", revs.Revisions[0].Artifacts)
	}
	owner.fail("GET", silent+"/revisions/1/captions.srt", nil, 404, "NotFound")

	stranger := srv.signedIn(t, "stranger@example.com")
	stranger.workspace("other")
	stranger.fail("GET", deliverable+"/revisions/1/captions.srt", nil, 404, "WorkspaceNotFound")
}
