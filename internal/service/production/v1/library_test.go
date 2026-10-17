package production_test

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ekin/internal/agenttool"
	"ekin/internal/apperror"
	"ekin/internal/blob"
	librarydomain "ekin/internal/domain/library/v1"
	pipelinedomain "ekin/internal/domain/pipeline/v1"
	storydomain "ekin/internal/domain/story/v1"
	libraryrepo "ekin/internal/repository/library/v1"
	librarysvc "ekin/internal/service/library/v1"
	production "ekin/internal/service/production/v1"
	"ekin/pkg/id"
)

// opusProcessor "ingests" any upload as a minute of Opus.
type opusProcessor struct{}

func (opusProcessor) Ingest(_ context.Context, _ librarydomain.Kind, _, outDir string) (librarydomain.Media, error) {
	return librarydomain.Media{Format: "opus", Duration: time.Minute}, os.WriteFile(filepath.Join(outDir, "main.opus"), []byte("opus"), 0o600)
}

func (opusProcessor) Inspect(_ context.Context, kind librarydomain.Kind, in, outDir string) (librarydomain.Media, error) {
	m := librarydomain.Media{Format: strings.TrimPrefix(filepath.Ext(in), "."), Width: 1080, Height: 1920, Poster: true}
	if kind == librarydomain.KindVideo {
		m.Duration = 10 * time.Second
	}
	return m, os.WriteFile(filepath.Join(outDir, "poster.jpg"), []byte("poster"), 0o600)
}

func (opusProcessor) Preview(_ context.Context, _, out string) error {
	return os.WriteFile(out, []byte("preview"), 0o600)
}

// blobFetcher fetches library files from the fixture's blob store, as the
// tool does on the server's own host.
type blobFetcher struct{ store blob.Store }

func (f blobFetcher) Fetch(ctx context.Context, key, dest string) error {
	rc, _, err := f.store.Open(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, raw, 0o600)
}

// A video whose music comes from the library only: its draft is given the
// library's music, the agent takes a track with ekin tool library, the
// render cuts that track under the voice, and the library counts the use.
func TestLibraryMusicInAVideo(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	library := newLibrary(t, f)
	track := ingestTrack(t, f, library, "sakin.mp3")
	tracks := &trackLog{}
	svc := f.hostWith(func(d *production.Deps) {
		d.Library = library
		d.Renderer = fakeRenderer{tracks: tracks}
	})
	f.agent.useLibrary = []string{track.ID().String()}
	f.agent.library = blobFetcher{f.blobs}

	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	req := production.AdHocRequest{Idea: "Kaygı ve endişe farkı", Count: 1, Language: pipelinedomain.Turkish, Outputs: outputs,
		Production: pipelinedomain.Production{Media: librarydomain.Policy{Music: librarydomain.ModeLibrary}}}
	if _, err := svc.QueueAdHoc(ctx, f.ws, id.New(), req); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)

	st := f.story(t)
	if st.Lead().Status() != storydomain.StatusReview {
		t.Fatalf("lead = %s: %s", st.Lead().Status(), st.Lead().Failure())
	}
	var spec agenttool.Spec
	for _, req := range f.agent.prompts {
		for _, kv := range req.Env {
			if raw, ok := strings.CutPrefix(kv, "EKIN_TOOL_SPEC="); ok {
				_ = json.Unmarshal([]byte(raw), &spec)
			}
		}
	}
	if spec.Media.Music != librarydomain.ModeLibrary {
		t.Fatalf("the draft's tool spec = %+v", spec.Media)
	}
	// Cut from the start, for the composition's length.
	want := "library-" + track.ID().String() + ".opus@0+21"
	if calls := tracks.calls; !slices.Equal(calls, []string{want}) {
		t.Fatalf("tracks cut = %v, want [%s]", calls, want)
	}
	uses, err := library.Usages(ctx, f.ws, track.ID())
	if err != nil || len(uses) != 1 || uses[0].StoryID != st.ID() || uses[0].Revision != 1 {
		t.Fatalf("usages = %+v, %v", uses, err)
	}
}

// A draft that may not use the library has no catalog, and the tool says
// so.
func TestFreshDraftHasNoCatalog(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	library := newLibrary(t, f)
	ingestTrack(t, f, library, "sakin.mp3")
	svc := f.hostWith(func(d *production.Deps) { d.Library = library })
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	if _, err := svc.QueueAdHoc(ctx, f.ws, id.New(), production.AdHocRequest{Idea: "Kaygı", Count: 1,
		Language: pipelinedomain.Turkish, Outputs: outputs}); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)
	st := f.story(t)
	if _, err := os.Stat(filepath.Join(svc.WorkDir(st.Lead().ID()), agenttool.CatalogPath)); err == nil {
		t.Fatal("a fresh draft got a library catalog")
	}
}

func newLibrary(t *testing.T, f fixture) *librarysvc.Service {
	t.Helper()
	return librarysvc.NewService(libraryrepo.NewSQLite(f.tx), f.blobs, f.jobs, opusProcessor{}, f.tx)
}

func ingestTrack(t *testing.T, f fixture, library *librarysvc.Service, name string) *librarydomain.Asset {
	t.Helper()
	ctx := context.Background()
	a, err := library.Upload(ctx, f.ws, id.New(), "", librarydomain.Details{Mood: "calm"}, name, strings.NewReader("mp3 "+name))
	if err != nil {
		t.Fatal(err)
	}
	if err := library.Ingest(ctx, a.ID()); err != nil {
		t.Fatal(err)
	}
	return a
}

// draftMedia is what a video draft holds: Pexels clips its composition
// shows (by id) and a composed score.
func draftMedia(clips ...string) map[string]string {
	var credits []agenttool.Credit
	files := map[string]string{
		"music.js":        "window.ekinMusic = () => {}",
		"music.meta.json": `{"title":"Sakin piyano","mood":"calm","tempo":70}`,
	}
	page := `<script src="/kit/ekin.js"></script>`
	for _, id := range clips {
		name := "assets/pexels-video-" + id + ".webm"
		files[name] = "clip " + id
		page += `<video src="` + name + `"></video>`
		credits = append(credits, agenttool.Credit{File: name, Source: "Pexels", Kind: "video", ExternalID: id, Author: "Ana",
			URL: "https://www.pexels.com/video/" + id + "/", Alt: "sea at dusk", Query: "sea", OriginalURL: "https://videos.pexels.com/" + id + ".mp4"})
	}
	raw, _ := json.Marshal(credits)
	files["assets/credits.json"] = string(raw)
	files["composition.html"] = page
	return files
}

// With saving on, each finished draft saves its Pexels clips and score as
// provisional; approving the video keeps what the approved revision uses
// and deletes what a later revision replaced.
func TestDraftsSaveToTheLibrary(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	library := newLibrary(t, f)
	if _, err := library.SaveCollect(ctx, f.ws, librarydomain.Collect{Music: true, Video: true}); err != nil {
		t.Fatal(err)
	}
	svc := f.hostWith(func(d *production.Deps) { d.Library = library })
	f.agent.files = draftMedia("1")

	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	if _, err := svc.QueueAdHoc(ctx, f.ws, id.New(), production.AdHocRequest{Idea: "Kaygı", Count: 1,
		Language: pipelinedomain.Turkish, Outputs: outputs}); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)
	saved := func() map[string]*librarydomain.Asset {
		t.Helper()
		list, _, err := library.List(ctx, f.ws, librarysvc.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*librarydomain.Asset{}
		for _, a := range list {
			out[cmp.Or(a.Harvest().SourceRef, string(a.Origin()))] = a
		}
		return out
	}
	got := saved()
	clip1, score := got["pexels:video:1"], got["generated"]
	if len(got) != 2 || clip1 == nil || score == nil || !clip1.Provisional() || !score.Provisional() {
		t.Fatalf("after the first draft = %v", got)
	}
	if clip1.Title() != "Sea at dusk" || clip1.Credit().Licence != "Pexels License" || clip1.Harvest().OriginalURL != "https://videos.pexels.com/1.mp4" ||
		score.Title() != "Sakin piyano" || score.Mood() != "calm" || score.Tempo() != 70 {
		t.Fatalf("details = %+v / %+v", clip1.Snapshot(), score.Snapshot())
	}

	// The second draft swaps the clip; the score stays the same file.
	st := f.story(t)
	f.agent.files = draftMedia("2")
	fb, _ := storydomain.NewFeedback([]storydomain.Note{{Text: "Başka bir deniz görüntüsü"}}, id.New(), time.Now())
	if _, err := svc.RequestChanges(ctx, f.ws, st.ID(), st.Lead().ID(), fb); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)
	if got := saved(); len(got) != 3 || got["pexels:video:2"] == nil {
		t.Fatalf("after the second draft = %v", got)
	}

	if _, err := svc.Approve(ctx, f.ws, st.ID(), st.Lead().ID(), id.New()); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)
	got = saved()
	if len(got) != 2 || got["pexels:video:1"] != nil || got["pexels:video:2"] == nil || got["generated"] == nil {
		t.Fatalf("after approval = %v", got)
	}
	for ref, a := range got {
		if a.Provisional() {
			t.Fatalf("%s still provisional", ref)
		}
	}
}

// Saving off (the default) saves nothing.
func TestDraftsSaveNothingByDefault(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	library := newLibrary(t, f)
	svc := f.hostWith(func(d *production.Deps) { d.Library = library })
	f.agent.files = draftMedia("1")
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	if _, err := svc.QueueAdHoc(ctx, f.ws, id.New(), production.AdHocRequest{Idea: "Kaygı", Count: 1,
		Language: pipelinedomain.Turkish, Outputs: outputs}); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)
	if list, _, _ := library.List(ctx, f.ws, librarysvc.Filter{}); len(list) != 0 {
		t.Fatalf("saved = %d", len(list))
	}
}

// A revision's media can be listed and saved by hand, whatever the
// workspace saves on its own.
func TestSaveARevisionsMedia(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	library := newLibrary(t, f)
	svc := f.hostWith(func(d *production.Deps) { d.Library = library })
	f.agent.files = draftMedia("7")
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	if _, err := svc.QueueAdHoc(ctx, f.ws, id.New(), production.AdHocRequest{Idea: "Kaygı", Count: 1,
		Language: pipelinedomain.Turkish, Outputs: outputs}); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)
	st := f.story(t)
	lead := st.Lead()

	media, err := svc.Media(ctx, f.ws, st.ID(), lead.ID(), 1)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, m := range media {
		keys = append(keys, m.Key+"/"+m.Source)
		if m.Asset != nil {
			t.Fatalf("%s already in the library", m.Key)
		}
	}
	if !slices.Equal(keys, []string{"pexels:video:7/pexels", "music.js/composed"}) {
		t.Fatalf("media = %v", keys)
	}

	a, err := svc.SaveMedia(ctx, f.ws, st.ID(), lead.ID(), 1, "pexels:video:7", id.New())
	if err != nil {
		t.Fatal(err)
	}
	if !a.Kept() || a.Provisional() || a.Harvest().SourceRef != "pexels:video:7" {
		t.Fatalf("saved = %+v", a.Snapshot())
	}
	media, _ = svc.Media(ctx, f.ws, st.ID(), lead.ID(), 1)
	if media[0].Asset == nil || media[0].Asset.ID() != a.ID() {
		t.Fatalf("saved clip not found again: %+v", media[0])
	}
	if _, err := svc.SaveMedia(ctx, f.ws, st.ID(), lead.ID(), 1, "pexels:video:99", id.New()); !apperror.Is(err, apperror.NotFound) {
		t.Fatalf("unknown key err = %v", err)
	}
	if _, err := svc.Media(ctx, f.ws, st.ID(), lead.ID(), 5); !apperror.Is(err, apperror.NotFound) {
		t.Fatalf("missing revision err = %v", err)
	}
	if _, err := svc.Media(ctx, id.New(), st.ID(), lead.ID(), 1); !apperror.Is(err, apperror.StoryNotFound) {
		t.Fatalf("other workspace err = %v", err)
	}
}
