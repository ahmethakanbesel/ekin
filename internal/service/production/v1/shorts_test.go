package production_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	spec, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatLong, pipelinedomain.Horizontal, 10*time.Minute)
	return longVideoOf(t, f, spec, length, live, files)
}

// longVideoOf is longVideo with the long video's spec.
func longVideoOf(t *testing.T, f fixture, spec pipelinedomain.VideoSpec, length time.Duration, live bool, files map[string][]byte) (*storydomain.Story, uuid.UUID) {
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

// wavFile is a mono 16-bit PCM WAV of seconds at 1000 frames a second.
func wavFile(seconds int) []byte {
	data := make([]byte, seconds*1000*2)
	for i := range seconds * 1000 {
		data[2*i] = 100
	}
	h := []byte("RIFF")
	h = binary.LittleEndian.AppendUint32(h, uint32(36+len(data)))
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1)    // PCM
	h = binary.LittleEndian.AppendUint16(h, 1)    // mono
	h = binary.LittleEndian.AppendUint32(h, 1000) // rate
	h = binary.LittleEndian.AppendUint32(h, 2000) // bytes per second
	h = binary.LittleEndian.AppendUint16(h, 2)
	h = binary.LittleEndian.AppendUint16(h, 16)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	return append(h, data...)
}

func TestShortCutFromALongVideoDraftsFromItsSlice(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	// Back-to-back 5.1 s sentences, two words each, so a four-sentence
	// moment lasts 20.4 s and fits the fake renderer's 21 s composition.
	var words, segs []map[string]any
	for i := range 120 {
		s := float64(i) * 5.1
		words = append(words,
			map[string]any{"text": fmt.Sprintf("Söz%d", i), "start": s, "end": s + 2},
			map[string]any{"text": "bitti.", "start": s + 2.5, "end": s + 5})
		segs = append(segs, map[string]any{"text": fmt.Sprintf("Söz%d bitti.", i), "start": s, "end": s + 5.1})
	}
	timings, _ := json.Marshal(map[string]any{"engine": "okur", "voice": "v1", "duration": 612, "words": words, "segments": segs})
	st, long := longVideo(t, f, 10*time.Minute, true, map[string][]byte{
		"narration/speech.wav":       wavFile(612),
		"narration/timings.json":     timings,
		"composition.html":           []byte("<!-- long layout -->"),
		"scenes.json":                []byte(`[{"at":0}]`),
		"music.js":                   []byte("// score"),
		"assets/pexels-video-1.webm": []byte("webm"),
		"assets/credits.json":        []byte(`[{"file":"assets/pexels-video-1.webm"}]`),
	})

	c, _ := storydomain.NewClip(sec(51), sec(71.4))
	_, m, err := f.svc.AddMoment(ctx, f.ws, st.ID(), long, storydomain.MomentDraft{Clip: c, Hook: "Öfkeyi adlandır"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.MakeShorts(ctx, f.ws, st.ID(), long, []uuid.UUID{m.ID()})
	if err != nil {
		t.Fatal(err)
	}
	shorts := got.Shorts(long)
	if len(shorts) != 1 || shorts[0].Status() != storydomain.StatusQueued {
		t.Fatalf("shorts = %+v", shorts)
	}
	f.drain(t)

	got, _ = f.svc.Get(ctx, f.ws, st.ID())
	short, _ := got.Deliverable(shorts[0].ID())
	if short.Status() != storydomain.StatusReview {
		t.Fatalf("short = %s (%s)", short.Status(), short.Failure())
	}
	if len(f.agent.clipDirs) != 1 {
		t.Fatalf("clip drafts = %d", len(f.agent.clipDirs))
	}
	dir := f.agent.clipDirs[0]
	var sliced struct {
		Engine   string  `json:"engine"`
		Voice    string  `json:"voice"`
		Duration float64 `json:"duration"`
		Words    []struct {
			Text       string
			Start, End float64
		} `json:"words"`
		Segments []struct{ Text string } `json:"segments"`
	}
	if err := json.Unmarshal([]byte(dir["narration/timings.json"]), &sliced); err != nil {
		t.Fatal(err)
	}
	if sliced.Engine != "okur" || sliced.Voice != "v1" || sliced.Duration != 20.4 || len(sliced.Segments) != 4 ||
		sliced.Segments[0].Text != "Söz10 bitti." || len(sliced.Words) != 8 || sliced.Words[1].Start != 2.5 {
		t.Fatalf("sliced timings = %+v", sliced)
	}
	if n := len(dir["narration/speech.wav"]); n != 44+20400*2 {
		t.Fatalf("sliced narration = %d bytes, want %d", n, 44+20400*2)
	}
	if dir["narration/narration.txt"] != "Söz10 bitti. Söz11 bitti. Söz12 bitti. Söz13 bitti.\n" {
		t.Fatalf("narration.txt = %q", dir["narration/narration.txt"])
	}
	for name, want := range map[string]string{
		"assets/pexels-video-1.webm":    "webm",
		"music.js":                      "// score",
		"sources/long/composition.html": "<!-- long layout -->",
		"sources/long/scenes.json":      `[{"at":0}]`,
	} {
		if dir[name] != want {
			t.Fatalf("%s = %q, want %q", name, dir[name], want)
		}
	}
	for name, want := range map[string]string{
		"INSTRUCTIONS.md": "0:51–1:11 of the long",
		"BRIEF.md":        "**Cut from:**",
		"sources/clip.md": "▶ 3. 15.00–20.10 s: Söz10 bitti.",
		"clip/clip.json":  `"windowStart": 36`,
		"sources/lead.md": "# Video script",
		"KIT.md":          "Kurgu.captions",
	} {
		if !strings.Contains(dir[name], want) {
			t.Fatalf("%s lacks %q:\n%s", name, want, dir[name])
		}
	}
	if strings.Contains(dir["INSTRUCTIONS.md"], "{{") {
		t.Fatal("INSTRUCTIONS.md has unfilled template fields")
	}

	// The narration is the long video's: a reviewer cannot rewrite it.
	n := "Başka bir metin."
	if _, err := f.svc.EditContent(ctx, f.ws, st.ID(), short.ID(), id.New(), production.Edit{Revision: short.Revision(), Narration: &n}); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("editing a short's narration: %v", err)
	}
}

func TestShortsDraftMayMoveItsCut(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		move     []string
		wantClip [2]float64
	}{
		// Window sentences 1–3 start at 40.8, 45.9 and 51 s of the long video.
		{"a sentence earlier", []string{"--first", "2", "--last", "6"}, [2]float64{45.9, 71.4}},
		{"kept", nil, [2]float64{51, 71.4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			var segs []map[string]any
			for i := range 120 {
				s := float64(i) * 5.1
				segs = append(segs, map[string]any{"text": fmt.Sprintf("Söz%d bitti.", i), "start": s, "end": s + 5.1})
			}
			timings, _ := json.Marshal(map[string]any{"engine": "okur", "duration": 612, "words": []any{}, "segments": segs})
			st, long := longVideo(t, f, 10*time.Minute, true, map[string][]byte{
				"narration/speech.wav": wavFile(612), "narration/timings.json": timings,
			})
			c, _ := storydomain.NewClip(sec(51), sec(71.4))
			_, m, err := f.svc.AddMoment(ctx, f.ws, st.ID(), long, storydomain.MomentDraft{Clip: c})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.svc.MakeShorts(ctx, f.ws, st.ID(), long, []uuid.UUID{m.ID()}); err != nil {
				t.Fatal(err)
			}
			f.agent.moveClip = tt.move
			f.drain(t)

			got, _ := f.svc.Get(ctx, f.ws, st.ID())
			short := got.Shorts(long)[0]
			if short.Status() != storydomain.StatusReview {
				t.Fatalf("short = %s (%s)", short.Status(), short.Failure())
			}
			want, _ := storydomain.NewClip(sec(tt.wantClip[0]), sec(tt.wantClip[1]))
			moment, _ := got.Moment(m.ID())
			if *short.Clip() != want || moment.Clip() != want {
				t.Fatalf("short clip %v, moment %v, want %v", short.Clip(), moment.Clip(), want)
			}
			if short.Video().Duration() != want.Length().Round(time.Second) {
				t.Fatalf("short length = %s", short.Video().Duration())
			}
		})
	}
}

func TestAgentFindsMomentsOfALongVideo(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("Uzun bir başlık ", 20)
	answer := `{"moments": [
		{"first": 3, "last": 8, "hook": "Öfkeyi adlandır", "reason": "Tek başına anlaşılıyor.", "score": 90},
		{"first": 20, "last": 26, "hook": "` + long + `", "reason": "Somut bir sahne.", "score": 70},
		{"first": 22, "last": 27, "hook": "Çakışan", "reason": "x", "score": 60},
		{"first": 0, "last": 3, "hook": "Sıfırdan", "reason": "x", "score": 99},
		{"first": 100, "last": 200, "hook": "Dışarıda", "reason": "x", "score": 99},
		{"first": 50, "last": 52, "hook": "Kısa", "reason": "x", "score": 95}
	]}`
	tests := []struct {
		name       string
		make       int
		wantShorts int
	}{
		{"suggests only", 0, 0},
		{"makes the best", 1, 1},
		{"makes no more than it found", 5, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.agent.moments = answer
			st, long := longVideo(t, f, 10*time.Minute, true, map[string][]byte{
				"narration/timings.json": timingsJSON(600), "script.md": []byte("Sahne 1: Trafikte öfke."),
				"metadata.json": []byte(`{"title":"Duyguyu adlandırmak"}`),
			})
			if tt.make == 0 {
				if _, err := f.svc.ScanMoments(ctx, f.ws, st.ID(), long); err != nil {
					t.Fatal(err)
				}
				f.drain(t)
			} else if err := f.svc.FindMoments(ctx, production.MomentsPayload{StoryID: st.ID(), SourceID: long, Make: tt.make}); err != nil {
				t.Fatal(err)
			}

			if len(f.agent.momentPrompts) != 1 {
				t.Fatalf("moment requests = %d", len(f.agent.momentPrompts))
			}
			prompt := f.agent.momentPrompts[0]
			for _, want := range []string{"3. [10.0–14.6] Cümle 3.", "lasts 20–60 seconds", "at least 30 seconds apart", "Sahne 1: Trafikte öfke.", "Duyguyu adlandırmak"} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("prompt lacks %q:\n%s", want, prompt)
				}
			}
			got, _ := f.svc.Get(ctx, f.ws, st.ID())
			moments := got.Moments(long)
			if len(moments) != 2 {
				t.Fatalf("moments = %d, want 2", len(moments))
			}
			first, second := moments[0], moments[1]
			if first.Clip().Start() != 10*time.Second || first.Clip().End() != sec(39.6) || first.Score() != 90 ||
				first.Hook() != "Öfkeyi adlandır" || first.Reason() != "Tek başına anlaşılıyor." || first.Origin() != storydomain.MomentByAgent {
				t.Fatalf("first moment = %v %d %q", first.Clip(), first.Score(), first.Hook())
			}
			if second.Clip().Start() != 95*time.Second || len([]rune(second.Hook())) != storydomain.MaxHookLength {
				t.Fatalf("second moment = %v, hook of %d runes", second.Clip(), len([]rune(second.Hook())))
			}
			shorts := got.Shorts(long)
			if len(shorts) != tt.wantShorts {
				t.Fatalf("shorts = %d, want %d", len(shorts), tt.wantShorts)
			}
			if tt.wantShorts > 0 && (*shorts[0].Clip() != first.Clip() || shorts[0].Status() != storydomain.StatusQueued) {
				t.Fatalf("best short = %v %s", shorts[0].Clip(), shorts[0].Status())
			}
		})
	}
}

func TestScanNeedsALiveLongVideo(t *testing.T) {
	f := newFixture(t)
	st, long := longVideo(t, f, 10*time.Minute, false, map[string][]byte{"narration/timings.json": timingsJSON(600)})
	if _, err := f.svc.ScanMoments(context.Background(), f.ws, st.ID(), long); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("scan of a video in review = %v", err)
	}
}

func TestLongVideoGoingLiveMakesTheShortsItAsksFor(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		narration  float64 // seconds; the render lasts about as long
		wantScans  int
		wantShorts int
	}{
		{"long enough", 600, 1, 2},
		{"shorter than the source minimum", 150, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.agent.moments = `{"moments": [
				{"first": 3, "last": 8, "hook": "Bir", "reason": "x", "score": 90},
				{"first": 20, "last": 26, "hook": "İki", "reason": "x", "score": 80},
				{"first": 40, "last": 46, "hook": "Üç", "reason": "x", "score": 70}
			]}`
			spec, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatLong, pipelinedomain.Horizontal, 10*time.Minute)
			spec, _ = spec.WithShorts(2)
			st, long := longVideoOf(t, f, spec, sec(tt.narration+0.5), false, map[string][]byte{
				"narration/speech.wav": wavFile(int(tt.narration)), "narration/timings.json": timingsJSON(tt.narration),
				"composition.html": []byte("<!-- long -->"),
			})
			if _, err := f.svc.Approve(ctx, f.ws, st.ID(), long, id.New()); err != nil {
				t.Fatal(err)
			}
			f.drain(t)

			got, _ := f.svc.Get(ctx, f.ws, st.ID())
			lead, _ := got.Deliverable(long)
			if lead.Status() != storydomain.StatusPublished {
				t.Fatalf("long video = %s (%s)", lead.Status(), lead.Failure())
			}
			if len(f.agent.momentPrompts) != tt.wantScans {
				t.Fatalf("moment scans = %d, want %d", len(f.agent.momentPrompts), tt.wantScans)
			}
			shorts := got.Shorts(long)
			if len(shorts) != tt.wantShorts {
				t.Fatalf("shorts = %d, want %d", len(shorts), tt.wantShorts)
			}
			for _, d := range shorts {
				if d.Status() != storydomain.StatusReview {
					t.Fatalf("short = %s (%s)", d.Status(), d.Failure())
				}
			}
		})
	}
}
