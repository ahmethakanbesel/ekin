package production_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"kurgu/internal/agenttool"
	"kurgu/internal/blob"
	librarydomain "kurgu/internal/domain/library/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	libraryrepo "kurgu/internal/repository/library/v1"
	librarysvc "kurgu/internal/service/library/v1"
	production "kurgu/internal/service/production/v1"
	"kurgu/pkg/id"
)

// opusProcessor "ingests" any upload as a minute of Opus.
type opusProcessor struct{}

func (opusProcessor) Ingest(_ context.Context, _ librarydomain.Kind, _, outDir string) (librarydomain.Media, error) {
	return librarydomain.Media{Format: "opus", Duration: time.Minute}, os.WriteFile(filepath.Join(outDir, "main.opus"), []byte("opus"), 0o600)
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
// library's music, the agent takes a track with kurgu tool library, the
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
			if raw, ok := strings.CutPrefix(kv, "KURGU_TOOL_SPEC="); ok {
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
