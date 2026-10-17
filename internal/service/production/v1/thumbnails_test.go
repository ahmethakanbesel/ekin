package production_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	production "kurgu/internal/service/production/v1"
	"kurgu/pkg/archive"
	"kurgu/pkg/id"
)

// draftLongVideo queues an ad-hoc long video with production settings p and
// drains it to review.
func draftLongVideo(t *testing.T, f fixture, svc *production.Service, p pipelinedomain.Production) *storydomain.Story {
	t.Helper()
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatLong, pipelinedomain.Vertical, 2*time.Minute)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	req := production.AdHocRequest{Idea: "Kaygı ve endişe farkı", Count: 1, Language: pipelinedomain.Turkish, Outputs: outputs, Production: p}
	if _, err := svc.QueueAdHoc(context.Background(), f.ws, id.New(), req); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)
	return f.story(t)
}

// artifact reads a revision's stored artifact.
func artifact(t *testing.T, f fixture, st *storydomain.Story, d *storydomain.Deliverable, rev int, name string) string {
	t.Helper()
	rc, _, err := f.svc.Artifact(context.Background(), f.ws, st.ID(), d.ID(), rev, name)
	if err != nil {
		t.Fatalf("%s of r%d: %v", name, rev, err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)
	return string(b)
}

func TestThumbnailVariants(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	var published []string
	svc := f.svc.AddPublisher(fakePublisher{kind: pipelinedomain.KindVideo, thumbnails: &published})
	st := draftLongVideo(t, f, svc, pipelinedomain.Production{})
	d := st.Lead()
	if d.Status() != storydomain.StatusReview || d.Revision() != 1 {
		t.Fatalf("long video: r%d %s (%s)", d.Revision(), d.Status(), d.Failure())
	}

	// The draft was asked for the default three variants, and each was drawn.
	draft := f.agent.prompts[len(f.agent.prompts)-1]
	instructions, _ := os.ReadFile(filepath.Join(draft.Dir, "INSTRUCTIONS.md")) //nolint:gosec // test dir
	kit, _ := os.ReadFile(filepath.Join(draft.Dir, "KIT.md"))                   //nolint:gosec // test dir
	if !strings.Contains(string(instructions), "Design 3 thumbnail variants") || !strings.Contains(string(kit), "Kurgu.thumbnails") {
		t.Fatalf("instructions do not ask for thumbnails:\n%s", instructions)
	}
	revs, _ := f.svc.Revisions(ctx, f.ws, st.ID(), d.ID())
	r1 := revs[0]
	if r1.Thumbnails.Recommended != 3 || len(r1.Thumbnails.Variants) != 3 || r1.Thumbnails.Variants[0].Text != "Söz 1" {
		t.Fatalf("revision thumbnails = %+v", r1.Thumbnails)
	}
	for n := 1; n <= 3; n++ {
		if got := artifact(t, f, st, d, 1, storydomain.ThumbnailArtifact(n)); !strings.HasPrefix(got, "thumb ") {
			t.Fatalf("thumb-%d = %q", n, got)
		}
	}
	if !slices.Contains(r1.ArtifactNames(), storydomain.ArtifactThumbnailsZip) {
		t.Fatalf("artifact names = %v", r1.ArtifactNames())
	}
	zipped := artifact(t, f, st, d, 1, storydomain.ArtifactThumbnailsZip)
	zr, err := zip.NewReader(bytes.NewReader([]byte(zipped)), int64(len(zipped)))
	if err != nil || len(zr.File) != 3 || zr.File[0].Name != "thumbnail-1-type.jpg" {
		t.Fatalf("zip = %v, %v", zr, err)
	}

	// Picking: only variants of the revision.
	if _, err := f.svc.PickThumbnail(ctx, f.ws, st.ID(), d.ID(), 4); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("pick 4 = %v", err)
	}
	if st, err = f.svc.PickThumbnail(ctx, f.ws, st.ID(), d.ID(), 2); err != nil || st.Lead().Thumbnail() != 2 {
		t.Fatalf("pick 2 = %v", err)
	}

	// Notes on the thumbnails alone: the agent changes only them, and the
	// video is not rendered again.
	fb, _ := storydomain.NewFeedback([]storydomain.Note{{Text: "Daha büyük yazı", Scope: storydomain.ScopeThumbnail}}, id.New(), time.Now())
	if _, err := f.svc.RequestChanges(ctx, f.ws, st.ID(), d.ID(), fb); err != nil {
		t.Fatal(err)
	}
	renders := &scaleLog{}
	svc = f.hostWith(func(deps *production.Deps) { deps.Renderer = fakeRenderer{scales: renders} }).
		AddPublisher(fakePublisher{kind: pipelinedomain.KindVideo, thumbnails: &published})
	f.drainOn(t, svc)
	st = f.story(t)
	d = st.Lead()
	if d.Status() != storydomain.StatusReview || d.Revision() != 2 || d.Thumbnail() != 0 {
		t.Fatalf("after thumbnail notes: r%d %s pick %d (%s)", d.Revision(), d.Status(), d.Thumbnail(), d.Failure())
	}
	if prompt := f.agent.prompts[len(f.agent.prompts)-1].Prompt; !strings.Contains(prompt, "Change only `thumbnail.html`") {
		t.Fatalf("revise prompt = %q", prompt)
	}
	if got := renders.all(); len(got) != 0 {
		t.Fatalf("the video was rendered again: %v", got)
	}
	revs, _ = f.svc.Revisions(ctx, f.ws, st.ID(), d.ID())
	r2 := revs[1]
	if r2.Artifacts[storydomain.ArtifactPreview] != r1.Artifacts[storydomain.ArtifactPreview] ||
		r2.Artifacts[storydomain.ArtifactPoster] != r1.Artifacts[storydomain.ArtifactPoster] || r2.Preview != r1.Preview {
		t.Fatalf("thumbnail revision did not keep the video: %v vs %v", r2.Artifacts, r1.Artifacts)
	}
	if r2.Thumbnails.Variants[0].Text != "Yeni 1" || !strings.Contains(artifact(t, f, st, d, 2, storydomain.ThumbnailArtifact(1)), "Yeni 1") {
		t.Fatalf("new thumbnails = %+v", r2.Thumbnails)
	}
	if d.Measures().Duration != r1.Measures.Duration || d.Measures().Duration == 0 {
		t.Fatalf("length after a thumbnail revision = %v, want %v", d.Measures().Duration, r1.Measures.Duration)
	}

	// Editing a variant's words in place: no agent, a new revision whose
	// thumbnails alone are drawn again, keeping the pick.
	prompts := len(f.agent.prompts)
	if st, err = f.svc.PickThumbnail(ctx, f.ws, st.ID(), d.ID(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.EditThumbnail(ctx, f.ws, st.ID(), d.ID(), id.New(), 1, 1, "Eski"); !apperror.Is(err, apperror.Conflict) {
		t.Fatalf("edit of an old revision = %v", err)
	}
	if _, err := f.svc.EditThumbnail(ctx, f.ws, st.ID(), d.ID(), id.New(), 2, 1, "bir iki üç dört beş altı"); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("six words = %v", err)
	}
	if _, err := f.svc.EditThumbnail(ctx, f.ws, st.ID(), d.ID(), id.New(), 2, 1, "  Kaygı  mı? "); err != nil {
		t.Fatal(err)
	}
	// Until the thumbnails are drawn again, the video and the old images
	// stand in.
	if pending, _ := f.svc.Revisions(ctx, f.ws, st.ID(), d.ID()); pending[2].Artifacts[storydomain.ArtifactPreview] != r1.Artifacts[storydomain.ArtifactPreview] ||
		pending[2].Artifacts[storydomain.ThumbnailArtifact(1)] != r2.Artifacts[storydomain.ThumbnailArtifact(1)] || pending[2].Preview != r1.Preview {
		t.Fatalf("revision being drawn = %+v", pending[2])
	}
	f.drainOn(t, svc)
	st = f.story(t)
	d = st.Lead()
	if d.Status() != storydomain.StatusReview || d.Revision() != 3 || d.Thumbnail() != 1 || len(f.agent.prompts) != prompts {
		t.Fatalf("after editing the words: r%d %s pick %d, agent turns %d → %d", d.Revision(), d.Status(), d.Thumbnail(), prompts, len(f.agent.prompts))
	}
	if got := renders.all(); len(got) != 0 {
		t.Fatalf("the video was rendered again: %v", got)
	}
	revs, _ = f.svc.Revisions(ctx, f.ws, st.ID(), d.ID())
	r3 := revs[2]
	if r3.Thumbnails.Variants[0].Text != "Kaygı mı?" || r3.Thumbnails.Variants[1].Text != "Yeni 2" || r3.EditedBy.String() == "" ||
		r3.Artifacts[storydomain.ArtifactPreview] != r1.Artifacts[storydomain.ArtifactPreview] {
		t.Fatalf("edited revision = %+v", r3)
	}
	// The work dir's thumbnails.json has the new words and keeps the rest.
	key := r3.Artifacts[storydomain.ArtifactWorkdir]
	rc, _, _ := f.blobs.Open(ctx, key)
	files, _ := archive.ReadFiles(rc, 1<<20, "thumbnails.json")
	_ = rc.Close()
	if j := string(files["thumbnails.json"]); !strings.Contains(j, `"Kaygı mı?"`) || !strings.Contains(j, `"note": "kept"`) {
		t.Fatalf("thumbnails.json = %s", j)
	}

	// Approving and publishing uploads the picked variant.
	if _, err := svc.Approve(ctx, f.ws, st.ID(), d.ID(), id.New()); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)
	if d = f.story(t).Lead(); d.Status() != storydomain.StatusPublished {
		t.Fatalf("after approval: %s (%s)", d.Status(), d.Failure())
	}
	if len(published) != 1 || !strings.Contains(published[0], "thumb 1: Kaygı mı?") {
		t.Fatalf("published thumbnails = %q", published)
	}
	if _, err := f.svc.PickThumbnail(ctx, f.ws, st.ID(), d.ID(), 2); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("pick after publishing = %v", err)
	}
}

func TestThumbnailsOffAndShorts(t *testing.T) {
	tests := []struct {
		name  string
		long  bool
		count *int
	}{
		{name: "a long video with thumbnails off", long: true, count: ptr(0)},
		{name: "a short", long: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			var published []string
			svc := f.svc.AddPublisher(fakePublisher{kind: pipelinedomain.KindVideo, thumbnails: &published})
			count, _ := pipelinedomain.ParseThumbnailCount(tt.count)
			var st *storydomain.Story
			if tt.long {
				st = draftLongVideo(t, f, svc, pipelinedomain.Production{Thumbnails: count})
			} else {
				video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
				outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
				if _, err := svc.QueueAdHoc(ctx, f.ws, id.New(), production.AdHocRequest{Idea: "Kaygı", Count: 1,
					Language: pipelinedomain.Turkish, Outputs: outputs}); err != nil {
					t.Fatal(err)
				}
				f.drainOn(t, svc)
				st = f.story(t)
			}
			d := st.Lead()
			instructions, _ := os.ReadFile(filepath.Join(f.agent.prompts[len(f.agent.prompts)-1].Dir, "INSTRUCTIONS.md")) //nolint:gosec // test dir
			if d.Status() != storydomain.StatusReview || strings.Contains(string(instructions), "thumbnail") {
				t.Fatalf("%s (%s); instructions mention thumbnails: %v", d.Status(), d.Failure(), strings.Contains(string(instructions), "thumbnail"))
			}
			revs, _ := f.svc.Revisions(ctx, f.ws, st.ID(), d.ID())
			if !revs[0].Thumbnails.IsZero() {
				t.Fatalf("thumbnails = %+v", revs[0].Thumbnails)
			}
			if _, err := svc.Approve(ctx, f.ws, st.ID(), d.ID(), id.New()); err != nil {
				t.Fatal(err)
			}
			f.drainOn(t, svc)
			if len(published) != 1 || published[0] != "jpg" {
				t.Fatalf("published thumbnails = %q, want the poster", published)
			}
		})
	}
}
