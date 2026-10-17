package httptransport_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	"kurgu/internal/gen"
	"kurgu/pkg/id"
)

// videoWithThumbnails stores a long video in review whose one revision
// has two thumbnail variants, the second recommended.
func (s *server) videoWithThumbnails(t *testing.T, ws uuid.UUID) (storyID, videoID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	thumbs, err := storydomain.ParseThumbnails([]byte(`{"recommended": 2, "variants": [
		{"n": 1, "concept": "frame", "text": "Kaygı mı?", "still": 12.5}, {"n": 2, "concept": "type", "text": "3 fark"}]}`), 2)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "test/" + id.New().String() + "/"
	artifacts := map[string]string{}
	for _, n := range []int{1, 2} {
		name := storydomain.ThumbnailArtifact(n)
		body := []byte(fmt.Sprintf("jpeg %d", n))
		if err := s.blobs.Put(ctx, prefix+name, bytes.NewReader(body), int64(len(body))); err != nil {
			t.Fatal(err)
		}
		artifacts[name] = prefix + name
	}
	spec, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatLong, pipelinedomain.Horizontal, 5*time.Minute)
	o, _ := pipelinedomain.NewOutputs(&spec, nil, "")
	idea, _ := storydomain.NewIdea("Kaygı ve endişe", "açı", nil)
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
	rev.Thumbnails = thumbs
	if err := s.stories.PutRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	_ = st.Rendered(video.ID(), 5*time.Minute, time.Now())
	if err := s.stories.Save(ctx, st); err != nil {
		t.Fatal(err)
	}
	return st.ID(), video.ID()
}

func TestThumbnailsAPI(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	storyID, videoID := srv.videoWithThumbnails(t, ws.Id)
	deliverable := fmt.Sprintf("/api/v1/workspaces/%s/stories/%s/deliverables/%s", ws.Id, storyID, videoID)

	var revs struct {
		Revisions []gen.Revision `json:"revisions"`
	}
	owner.call("GET", deliverable+"/revisions", nil, 200, &revs)
	th := revs.Revisions[0].Thumbnails
	if th == nil || th.Recommended != 2 || len(th.Variants) != 2 || th.Variants[0].Text != "Kaygı mı?" ||
		th.Variants[0].StillSeconds == nil || *th.Variants[0].StillSeconds != 12.5 || th.Variants[1].StillSeconds != nil {
		t.Fatalf("thumbnails = %+v", th)
	}
	res := owner.do("GET", deliverable+"/revisions/1/thumb-2.jpg", nil, nil)
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || string(body) != "jpeg 2" || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb-2.jpg = %d %q", res.StatusCode, body)
	}
	res = owner.do("GET", deliverable+"/revisions/1/thumbnails.zip?download=1", nil, nil)
	_ = res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("thumbnails.zip = %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}

	var st gen.Story
	owner.call("PUT", deliverable+"/thumbnail", map[string]any{"variant": 1}, 200, &st)
	if d := st.Deliverables[0]; d.Thumbnail == nil || *d.Thumbnail != 1 {
		t.Fatalf("picked = %+v", d.Thumbnail)
	}
	owner.fail("PUT", deliverable+"/thumbnail", map[string]any{"variant": 3}, 400, "ValidationFailed")
	var back gen.Story
	owner.call("PUT", deliverable+"/thumbnail", map[string]any{"variant": 0}, 200, &back)
	if d := back.Deliverables[0]; d.Thumbnail != nil {
		t.Fatalf("picked after going back to the recommended one = %v", *d.Thumbnail)
	}

	// Notes may be about the thumbnails alone.
	owner.call("POST", deliverable+"/changes", map[string]any{"notes": []map[string]any{
		{"text": "Daha büyük yazı", "scope": "thumbnail"}}}, 200, &st)
	if d := st.Deliverables[0]; d.PendingFeedback == nil || d.PendingFeedback.Notes[0].Scope != gen.FeedbackScopeThumbnail {
		t.Fatalf("pending = %+v", d.PendingFeedback)
	}
	owner.fail("PATCH", deliverable+"/thumbnails/1", map[string]any{"revision": 1, "text": "Yeni"}, 409, "InvalidTransition")
	owner.fail("PUT", deliverable+"/thumbnail", map[string]any{"variant": 1}, 409, "InvalidTransition")

	stranger := srv.signedIn(t, "stranger@example.com")
	stranger.workspace("other")
	stranger.fail("PUT", deliverable+"/thumbnail", map[string]any{"variant": 1}, 404, "WorkspaceNotFound")
	stranger.fail("PATCH", deliverable+"/thumbnails/1", map[string]any{"revision": 1, "text": "Yeni"}, 404, "WorkspaceNotFound")
}
