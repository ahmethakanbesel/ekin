package library_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/blob"
	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	librarydomain "kurgu/internal/domain/library/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	libraryrepo "kurgu/internal/repository/library/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	jobsvc "kurgu/internal/service/job/v1"
	librarysvc "kurgu/internal/service/library/v1"
	"kurgu/pkg/id"
)

var now = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

// processor writes fixed files, or fails, and may run during to act
// while it works.
type processor struct {
	media  librarydomain.Media
	err    error
	during func()
	calls  int
}

func (p *processor) Ingest(_ context.Context, _ librarydomain.Kind, in, outDir string) (librarydomain.Media, error) {
	p.calls++
	if p.during != nil {
		p.during()
	}
	if _, err := os.Stat(in); err != nil {
		return librarydomain.Media{}, err
	}
	if p.err != nil {
		return librarydomain.Media{}, p.err
	}
	_ = os.WriteFile(filepath.Join(outDir, "main."+p.media.Format), []byte("main"), 0o600)
	if p.media.Poster {
		_ = os.WriteFile(filepath.Join(outDir, "poster.jpg"), []byte("poster"), 0o600)
	}
	return p.media, nil
}

func (p *processor) Inspect(_ context.Context, kind librarydomain.Kind, in, outDir string) (librarydomain.Media, error) {
	if _, err := os.Stat(in); err != nil {
		return librarydomain.Media{}, err
	}
	_ = os.WriteFile(filepath.Join(outDir, "poster.jpg"), []byte("poster"), 0o600)
	m := librarydomain.Media{Format: strings.TrimPrefix(filepath.Ext(in), "."), Width: 1080, Height: 1920, Poster: true}
	if kind == librarydomain.KindVideo {
		m.Duration = 9 * time.Second
	}
	return m, nil
}

func (p *processor) Preview(_ context.Context, in, out string) error {
	if _, err := os.Stat(in); err != nil {
		return err
	}
	return os.WriteFile(out, []byte("preview"), 0o600)
}

type fixture struct {
	svc   *librarysvc.Service
	jobs  *jobsvc.Service
	blobs blob.Store
	proc  *processor
	ws    uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, now)
	if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	blobs, _ := blob.NewFS(t.TempDir())
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	proc := &processor{media: librarydomain.Media{Format: "webm", Width: 1080, Height: 1920, Duration: 8 * time.Second, Poster: true}}
	svc := librarysvc.NewService(libraryrepo.NewSQLite(tx), blobs, jobs, proc, tx).WithClock(func() time.Time { return now })
	return fixture{svc: svc, jobs: jobs, blobs: blobs, proc: proc, ws: w.ID()}
}

// runIngest runs the queued ingest job the way the worker would.
func (f fixture) runIngest(t *testing.T) error {
	t.Helper()
	j, err := f.jobs.Claim(context.Background(), []jobdomain.Queue{jobdomain.QueueExtract}, []string{librarysvc.KindIngest}, "test", time.Minute)
	if err != nil || j == nil {
		t.Fatalf("claim = %v, %v", j, err)
	}
	var p librarysvc.IngestPayload
	_ = json.Unmarshal(j.Payload(), &p)
	return f.svc.Ingest(context.Background(), p.AssetID)
}

func (f fixture) upload(t *testing.T, filename, content string) *librarydomain.Asset {
	t.Helper()
	a, err := f.svc.Upload(context.Background(), f.ws, id.New(), "", librarydomain.Details{}, filename, strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f fixture) exists(key string) bool {
	rc, _, err := f.blobs.Open(context.Background(), key)
	if err == nil {
		_ = rc.Close()
	}
	return err == nil
}

func TestUploadAndIngest(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.upload(t, "Deniz Kıyısı.mp4", "video bytes")
	if a.Kind() != librarydomain.KindVideo || a.Status() != librarydomain.StatusProcessing || a.Title() != "Deniz Kıyısı" ||
		a.Size() != 11 || len(a.SHA256()) != 64 || !f.exists(a.OriginalKey()) {
		t.Fatalf("uploaded = %+v", a.Snapshot())
	}
	if err := f.runIngest(t); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.Get(ctx, f.ws, a.ID())
	if got.Status() != librarydomain.StatusReady || got.Orientation() != librarydomain.OrientationPortrait || got.Duration() != 8*time.Second {
		t.Fatalf("ingested = %+v", got.Snapshot())
	}
	if f.exists(a.OriginalKey()) {
		t.Fatal("original kept after a successful ingest")
	}
	_, rc, size, err := f.svc.Open(ctx, f.ws, a.ID(), librarydomain.FilePoster)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(body) != "poster" || size != 6 {
		t.Fatalf("poster = %q (%d)", body, size)
	}
	if _, err := f.svc.Upload(ctx, f.ws, id.New(), "", librarydomain.Details{}, "copy.mp4", strings.NewReader("video bytes")); !apperror.Is(err, apperror.Conflict) ||
		!strings.Contains(apperror.Message(err), "Deniz Kıyısı") {
		t.Fatalf("duplicate upload err = %v", err)
	}
}

func TestUploadRefusals(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	tests := []struct {
		name     string
		kind     librarydomain.Kind
		filename string
		content  string
	}{
		{"unsupported type", "", "a.gif", "x"},
		{"empty", "", "a.mp3", ""},
		{"kind disagrees", librarydomain.KindPhoto, "a.mp3", "x"},
		{"photo too large", "", "a.jpg", strings.Repeat("x", librarydomain.MaxPhotoBytes+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.svc.Upload(ctx, f.ws, id.New(), tt.kind, librarydomain.Details{}, tt.filename, strings.NewReader(tt.content))
			if !apperror.Is(err, apperror.ValidationFailed) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if list, _, _ := f.svc.List(ctx, f.ws, librarysvc.Filter{}); len(list) != 0 {
		t.Fatalf("refused uploads stored: %d", len(list))
	}
}

func TestIngestFailureAndRetry(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.proc.err = errors.New("the file could not be read as media: moov atom not found")
	a := f.upload(t, "broken.mp4", "junk")
	if err := f.runIngest(t); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.Get(ctx, f.ws, a.ID())
	if got.Status() != librarydomain.StatusFailed || !strings.Contains(got.Failure(), "moov atom") || !f.exists(a.OriginalKey()) {
		t.Fatalf("failed = %+v, original kept %v", got.Snapshot(), f.exists(a.OriginalKey()))
	}
	if _, _, _, err := f.svc.Open(ctx, f.ws, a.ID(), librarydomain.FileMain); !apperror.Is(err, apperror.AssetNotFound) {
		t.Fatalf("open failed asset err = %v", err)
	}
	f.proc.err = nil
	if _, err := f.svc.Retry(ctx, f.ws, a.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Retry(ctx, f.ws, a.ID()); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("retry while processing err = %v", err)
	}
	if err := f.runIngest(t); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Get(ctx, f.ws, a.ID()); got.Status() != librarydomain.StatusReady {
		t.Fatalf("after retry = %s", got.Status())
	}
}

func TestIngestTooLongFailsWithoutStoring(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.proc.media.Duration = 5 * time.Minute
	a := f.upload(t, "long.mp4", "long")
	if err := f.runIngest(t); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.Get(ctx, f.ws, a.ID())
	if got.Status() != librarydomain.StatusFailed || f.exists(a.Prefix()+"/main.webm") {
		t.Fatalf("too long = %s, main stored %v", got.Status(), f.exists(a.Prefix()+"/main.webm"))
	}
}

func TestIngestKeepsEditsAndDeletions(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.upload(t, "clip.mp4", "one")
	f.proc.during = func() {
		title := "Renamed meanwhile"
		_, _ = f.svc.Update(ctx, f.ws, a.ID(), librarysvc.Patch{Details: &librarydomain.Details{Title: title, Tags: []string{"sea"}}})
	}
	if err := f.runIngest(t); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.Get(ctx, f.ws, a.ID())
	if got.Title() != "Renamed meanwhile" || got.Status() != librarydomain.StatusReady {
		t.Fatalf("after edit during ingest = %+v", got.Snapshot())
	}

	b := f.upload(t, "other.mp4", "two")
	f.proc.during = func() { _ = f.svc.Delete(ctx, f.ws, b.ID()) }
	if err := f.runIngest(t); !errors.Is(err, jobdomain.ErrPermanent) {
		t.Fatalf("ingest of a deleted asset err = %v", err)
	}
}

func TestUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.upload(t, "clip.mp4", "one")
	if err := f.runIngest(t); err != nil {
		t.Fatal(err)
	}
	archived := true
	got, err := f.svc.Update(ctx, f.ws, a.ID(), librarysvc.Patch{Archived: &archived})
	if err != nil || got.Usable() || got.Title() != "clip" {
		t.Fatalf("archive = %+v, %v", got, err)
	}
	if list, _, _ := f.svc.List(ctx, f.ws, librarysvc.Filter{}); len(list) != 0 {
		t.Fatalf("archived listed by default: %d", len(list))
	}
	if _, err := f.svc.Update(ctx, f.ws, a.ID(), librarysvc.Patch{Details: &librarydomain.Details{Title: "x", Mood: "calm"}}); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("mood on a video err = %v", err)
	}
	if _, err := f.svc.Update(ctx, id.New(), a.ID(), librarysvc.Patch{Archived: &archived}); !apperror.Is(err, apperror.AssetNotFound) {
		t.Fatalf("other workspace err = %v", err)
	}
	main, _ := got.Key(librarydomain.FileMain)
	if err := f.svc.Delete(ctx, f.ws, a.ID()); err != nil {
		t.Fatal(err)
	}
	if f.exists(main) {
		t.Fatal("files kept after delete")
	}
	if _, err := f.svc.Get(ctx, f.ws, a.ID()); !apperror.Is(err, apperror.AssetNotFound) {
		t.Fatalf("get deleted err = %v", err)
	}
}

func TestCatalogAndUsage(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ingest := func(name string, m librarydomain.Media) *librarydomain.Asset {
		t.Helper()
		f.proc.media = m
		a := f.upload(t, name, name)
		if err := f.runIngest(t); err != nil {
			t.Fatal(err)
		}
		return a
	}
	ingest("tall.mp4", librarydomain.Media{Format: "webm", Width: 1080, Height: 1920, Duration: time.Second})
	ingest("wide.mp4", librarydomain.Media{Format: "webm", Width: 1920, Height: 1080, Duration: time.Second})
	ingest("square.jpg", librarydomain.Media{Format: "jpg", Width: 1000, Height: 1000})
	used := ingest("used.mp4", librarydomain.Media{Format: "webm", Width: 1080, Height: 1920, Duration: time.Second})
	archived := ingest("old.mp4", librarydomain.Media{Format: "webm", Width: 1080, Height: 1920, Duration: time.Second})
	music := ingest("song.mp3", librarydomain.Media{Format: "opus", Duration: time.Minute})
	yes := true
	if _, err := f.svc.Update(ctx, f.ws, archived.ID(), librarysvc.Patch{Archived: &yes}); err != nil {
		t.Fatal(err)
	}
	story, deliverable := id.New(), id.New()
	if err := f.svc.RecordUsage(ctx, f.ws, story, deliverable, 1, []uuid.UUID{used.ID(), id.New()}); err != nil {
		t.Fatal(err)
	}

	entries, err := f.svc.Catalog(ctx, f.ws, []librarydomain.Kind{librarydomain.KindVideo, librarydomain.KindPhoto}, librarydomain.OrientationPortrait)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, fmt.Sprintf("%s:%d", e.Asset.Title(), e.Uses))
	}
	// Fitting, unarchived, least used first, then newest; no music, no wide clip.
	if want := []string{"square:0", "tall:0", "used:1"}; !slices.Equal(got, want) {
		t.Fatalf("catalog = %v, want %v", got, want)
	}
	if e, _ := f.svc.Catalog(ctx, f.ws, nil, librarydomain.OrientationPortrait); e != nil {
		t.Fatalf("no kinds = %v", e)
	}
	if e, _ := f.svc.Catalog(ctx, f.ws, []librarydomain.Kind{librarydomain.KindMusic}, librarydomain.OrientationLandscape); len(e) != 1 || e[0].Asset.ID() != music.ID() {
		t.Fatalf("music catalog = %v", e)
	}
	uses, err := f.svc.Usages(ctx, f.ws, used.ID())
	if err != nil || len(uses) != 1 || uses[0].StoryID != story || uses[0].Revision != 1 {
		t.Fatalf("usages = %+v, %v", uses, err)
	}
	if _, err := f.svc.Usages(ctx, id.New(), used.ID()); !apperror.Is(err, apperror.AssetNotFound) {
		t.Fatalf("usages in another workspace err = %v", err)
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func stock(t *testing.T, ref string, deliverable uuid.UUID, revision int) librarysvc.Harvestable {
	return librarysvc.Harvestable{
		Kind: librarydomain.KindVideo, Origin: librarydomain.OriginPexels,
		Harvest: librarydomain.Harvest{StoryID: id.New(), DeliverableID: deliverable, Revision: revision, SourceRef: ref},
		Details: librarydomain.Details{Title: "Sea", Credit: librarydomain.Credit{Author: "Ana"}},
		Path:    writeTemp(t, "clip.webm", "clip "+ref),
	}
}

func TestSaveOnce(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	deliverable := id.New()
	h := stock(t, "pexels:video:1", deliverable, 1)
	a, err := f.svc.Save(ctx, f.ws, uuid.Nil, h, false)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Provisional() || a.Kept() || !a.Usable() || a.Duration() != 9*time.Second {
		t.Fatalf("saved = %+v", a.Snapshot())
	}
	main, _ := a.Key(librarydomain.FileMain)
	poster, _ := a.Key(librarydomain.FilePoster)
	if !f.exists(main) || !f.exists(poster) {
		t.Fatal("files not stored")
	}
	if uses, _ := f.svc.Usages(ctx, f.ws, a.ID()); len(uses) != 1 || uses[0].DeliverableID != deliverable {
		t.Fatalf("the draft it came from is not a use: %+v", uses)
	}

	// The same Pexels clip again (a later revision) is the same asset; a
	// person saving it keeps it.
	again, err := f.svc.Save(ctx, f.ws, uuid.Nil, stock(t, "pexels:video:1", deliverable, 2), false)
	if err != nil || again.ID() != a.ID() || !again.Provisional() {
		t.Fatalf("saved again = %+v, %v", again, err)
	}
	kept, err := f.svc.Save(ctx, f.ws, id.New(), stock(t, "pexels:video:1", deliverable, 2), true)
	if err != nil || kept.ID() != a.ID() || !kept.Kept() || kept.Provisional() {
		t.Fatalf("saved by a person = %+v, %v", kept, err)
	}

	// A composed score is found again by its content, with its preview.
	score := librarysvc.Harvestable{
		Kind: librarydomain.KindMusic, Origin: librarydomain.OriginGenerated,
		Harvest: librarydomain.Harvest{StoryID: id.New(), DeliverableID: deliverable, Revision: 1},
		Details: librarydomain.Details{Title: "Calm", Mood: "calm"},
		Path:    writeTemp(t, "music.js", "window.kurguMusic = () => {}"), Preview: writeTemp(t, "music.wav", "RIFF"),
	}
	s1, err := f.svc.Save(ctx, f.ws, uuid.Nil, score, false)
	if err != nil {
		t.Fatal(err)
	}
	preview, ok := s1.Key(librarydomain.FilePreview)
	if !ok || !f.exists(preview) {
		t.Fatal("score preview not stored")
	}
	score.Path = writeTemp(t, "music.js", "window.kurguMusic = () => {}")
	if s2, err := f.svc.Save(ctx, f.ws, uuid.Nil, score, false); err != nil || s2.ID() != s1.ID() {
		t.Fatalf("same score twice = %v, %v", s2, err)
	}
}

func TestSettle(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	deliverable := id.New()
	save := func(ref string, revision int) *librarydomain.Asset {
		t.Helper()
		a, err := f.svc.Save(ctx, f.ws, uuid.Nil, stock(t, ref, deliverable, revision), false)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	replaced := save("pexels:video:1", 1)    // revision 1 used it, revision 2 swapped it out
	stays := save("pexels:video:2", 1)       // still in the decided revision
	kept := save("pexels:video:3", 1)        // a person kept it
	reused := save("pexels:video:4", 1)      // another video used it meanwhile
	decidedOnly := save("pexels:video:5", 2) // only the decided revision
	if _, err := f.svc.Update(ctx, f.ws, kept.ID(), librarysvc.Patch{Details: &librarydomain.Details{Title: "Mine", Credit: librarydomain.Credit{Author: "Ana"}}}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RecordUsage(ctx, f.ws, id.New(), id.New(), 1, []uuid.UUID{reused.ID()}); err != nil {
		t.Fatal(err)
	}
	other := id.New()
	foreign, err := f.svc.Save(ctx, f.ws, uuid.Nil, stock(t, "pexels:video:6", other, 1), false)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := f.svc.Settle(ctx, f.ws, deliverable, librarysvc.Decided{SourceRefs: []string{"pexels:video:2", "pexels:video:5"}})
	if err != nil || deleted != 1 {
		t.Fatalf("settle = %d, %v", deleted, err)
	}
	if _, err := f.svc.Get(ctx, f.ws, replaced.ID()); !apperror.Is(err, apperror.AssetNotFound) {
		t.Fatalf("replaced asset err = %v", err)
	}
	if main, _ := replaced.Key(librarydomain.FileMain); f.exists(main) {
		t.Fatal("replaced asset's files kept")
	}
	for _, a := range []*librarydomain.Asset{stays, kept, reused, decidedOnly} {
		got, err := f.svc.Get(ctx, f.ws, a.ID())
		if err != nil || got.Provisional() {
			t.Fatalf("%s = %+v, %v", a.Harvest().SourceRef, got, err)
		}
	}
	if got, _ := f.svc.Get(ctx, f.ws, foreign.ID()); !got.Provisional() {
		t.Fatal("another deliverable's asset was settled")
	}
	if n, err := f.svc.Settle(ctx, f.ws, deliverable, librarysvc.Decided{}); err != nil || n != 0 {
		t.Fatalf("settling again = %d, %v", n, err)
	}
}

func TestCollectSettings(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if c, err := f.svc.Collect(ctx, f.ws); err != nil || c.Any() {
		t.Fatalf("default = %+v, %v", c, err)
	}
	want := librarydomain.Collect{Music: true, Video: true}
	if _, err := f.svc.SaveCollect(ctx, f.ws, want); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.svc.Collect(ctx, f.ws); c != want {
		t.Fatalf("collect = %+v", c)
	}
}
