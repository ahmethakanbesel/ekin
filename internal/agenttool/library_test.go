package agenttool_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/pexels"
	"github.com/ahmethakanbesel/ekin/internal/agenttool"
	librarydomain "github.com/ahmethakanbesel/ekin/internal/domain/library/v1"
)

// fetcher "downloads" a key by writing its name.
type fetcher struct{ fetched []string }

func (f *fetcher) Fetch(_ context.Context, key, dest string) error {
	f.fetched = append(f.fetched, key)
	return os.WriteFile(dest, []byte(key), 0o600)
}

var catalog = []agenttool.CatalogItem{
	{ID: "v1", Kind: "video", Title: "Sea at dusk", Description: "Waves rolling in", Tags: []string{"sea", "calm"}, Seconds: 12, Width: 1080, Height: 1920, Format: "webm", Key: "k/v1"},
	{ID: "v2", Kind: "video", Title: "City traffic", Tags: []string{"city"}, Seconds: 8, Width: 1080, Height: 1920, Format: "webm", Key: "k/v2", Uses: 3},
	{ID: "p1", Kind: "photo", Title: "Dağ zirvesi", Width: 2000, Height: 2000, Format: "jpg", Key: "k/p1", Credit: librarydomain.Credit{Author: "Ayşe"}},
	{ID: "m1", Kind: "music", Title: "Calm piano", Mood: "calm", Tempo: 70, Seconds: 95, Format: "opus", Key: "k/m1", Credit: librarydomain.Credit{Author: "Ali", Licence: "CC BY"}},
	{ID: "m2", Kind: "music", Title: "Hopeful score", Format: "js", Key: "k/m2", SHA256: sha("k/m2")},
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func workDir(t *testing.T, items []agenttool.CatalogItem) string {
	t.Helper()
	dir := t.TempDir()
	if items != nil {
		_ = os.MkdirAll(filepath.Join(dir, "library"), 0o750)
		b, _ := json.Marshal(items)
		if err := os.WriteFile(filepath.Join(dir, agenttool.CatalogPath), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func run(t *testing.T, e agenttool.Env, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	e.Stdout, e.Stderr = &out, &out
	err := agenttool.Run(context.Background(), e, args)
	return out.String(), err
}

func TestLibrarySearch(t *testing.T) {
	e := agenttool.Env{Dir: workDir(t, catalog), Library: &fetcher{}}
	tests := []struct {
		name    string
		args    []string
		want    []string
		notWant []string
	}{
		{"all videos", []string{"library", "--type", "video"}, []string{"v1", "v2", "used 3×"}, []string{"p1", "m1"}},
		{"query in tags and description, folded", []string{"library", "--query", "WAVES dag"}, []string{"v1", "p1"}, []string{"v2", "m1"}},
		{"music with mood", []string{"library", "--type", "music", "--query", "calm"}, []string{"m1", "calm, 70 BPM"}, []string{"v1"}},
		{"nothing matches", []string{"library", "--query", "volcano"}, []string{"nothing in the library matches"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := run(t, e, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(out, w+"  ") {
					t.Errorf("output lists %q:\n%s", w, out)
				}
			}
		})
	}
	if _, err := run(t, agenttool.Env{Dir: workDir(t, nil)}, "library"); err == nil {
		t.Fatal("a draft without a catalog searched the library")
	}
	if _, err := run(t, e, "library", "--type", "gif"); err == nil {
		t.Fatal("bad type accepted")
	}
}

func readCredits(t *testing.T, dir string) []agenttool.Credit {
	t.Helper()
	var out []agenttool.Credit
	raw, _ := os.ReadFile(filepath.Join(dir, "assets", "credits.json"))
	_ = json.Unmarshal(raw, &out)
	return out
}

func TestLibraryUse(t *testing.T) {
	f := &fetcher{}
	dir := workDir(t, catalog)
	e := agenttool.Env{Dir: dir, Library: f}

	out, err := run(t, e, "library", "use", "v1")
	if err != nil || !strings.Contains(out, "assets/library-v1.webm") {
		t.Fatalf("use video = %q, %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "assets", "library-v1.webm")); err != nil {
		t.Fatal(err)
	}

	// A track replaces a composed score.
	_ = os.WriteFile(filepath.Join(dir, "music.js"), []byte("score"), 0o600)
	out, err = run(t, e, "library", "use", "m1")
	if err != nil || !strings.Contains(out, "music.js was removed") || !strings.Contains(out, "Ali · CC BY") {
		t.Fatalf("use track = %q, %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "music.js")); err == nil {
		t.Fatal("music.js kept beside a track")
	}
	track, ok, err := agenttool.ReadMusicTrack(dir)
	if !ok || err != nil || track.Asset != "m1" || track.File != "assets/library-m1.opus" {
		t.Fatalf("music.json = %+v %v %v", track, ok, err)
	}
	// Using it twice credits it once.
	if _, err := run(t, e, "library", "use", "m1"); err != nil {
		t.Fatal(err)
	}
	credits := readCredits(t, dir)
	if len(credits) != 2 || credits[1].Library != "m1" || credits[1].Kind != "music" || credits[1].Licence != "CC BY" {
		t.Fatalf("credits = %+v", credits)
	}
	if ids := agenttool.LibraryIDs(dir); !slices.Equal(ids, []string{"v1", "m1"}) {
		t.Fatalf("library ids = %v", ids)
	}

	// A library score replaces a track.
	if _, err := run(t, e, "library", "use", "m2"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := agenttool.ReadMusicTrack(dir); ok {
		t.Fatal("music.json kept beside a library score")
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "music.js")); string(raw) != "k/m2" {
		t.Fatalf("music.js = %q", raw)
	}

	if _, err := run(t, e, "library", "use", "nope"); err == nil {
		t.Fatal("unknown id used")
	}
	if _, err := run(t, agenttool.Env{Dir: dir}, "library", "use", "v2"); err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("use without a library = %v", err)
	}
}

func TestStockRefusedInLibraryMode(t *testing.T) {
	e := agenttool.Env{Dir: workDir(t, catalog), Spec: agenttool.Spec{Media: librarydomain.Policy{Video: librarydomain.ModeLibrary}}}
	if _, err := run(t, e, "stock", "--query", "sea", "--type", "video"); err == nil || !strings.Contains(err.Error(), "library only") {
		t.Fatalf("stock video in library mode = %v", err)
	}
}

func TestCheckMedia(t *testing.T) {
	write := func(dir, name, content string) {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o750)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	credits := func(cs ...agenttool.Credit) string {
		b, _ := json.Marshal(cs)
		return string(b)
	}
	libraryOnly := librarydomain.Policy{Music: librarydomain.ModeLibrary, Video: librarydomain.ModeLibrary, Photo: librarydomain.ModeLibrary}
	tests := []struct {
		name   string
		policy librarydomain.Policy
		files  map[string]string
		want   string // a substring of the only problem; "" for none
	}{
		{"fresh allows anything", librarydomain.Policy{}, map[string]string{
			"composition.html": `<video src="assets/pexels-video-1.webm">`, "music.js": "x",
			"assets/credits.json": credits(agenttool.Credit{File: "assets/pexels-video-1.webm", Source: "Pexels", Kind: "video"}),
		}, ""},
		{"pexels clip in library mode", libraryOnly, map[string]string{
			"composition.html":    `<video src="assets/pexels-video-1.webm">`,
			"assets/credits.json": credits(agenttool.Credit{File: "assets/pexels-video-1.webm", Source: "Pexels", Kind: "video"}),
		}, "library only"},
		{"old credit without kind", libraryOnly, map[string]string{
			"composition.html":    `<img src="assets/pexels-photo-9.jpg">`,
			"assets/credits.json": credits(agenttool.Credit{File: "assets/pexels-photo-9.jpg", Source: "Pexels"}),
		}, "photos from the library only"},
		{"unused pexels download is fine", libraryOnly, map[string]string{
			"composition.html":    `<div></div>`,
			"assets/credits.json": credits(agenttool.Credit{File: "assets/pexels-video-1.webm", Source: "Pexels", Kind: "video"}),
		}, ""},
		{"pexels photo allowed when only videos are library", librarydomain.Policy{Video: librarydomain.ModeLibrary}, map[string]string{
			"composition.html":    `<img src="assets/pexels-photo-9.jpg">`,
			"assets/credits.json": credits(agenttool.Credit{File: "assets/pexels-photo-9.jpg", Source: "Pexels", Kind: "photo"}),
		}, ""},
		{"library item neither in catalog nor here", libraryOnly, map[string]string{
			"composition.html":    `<video src="assets/library-zz.webm">`,
			"assets/credits.json": credits(agenttool.Credit{File: "assets/library-zz.webm", Source: "Library", Kind: "video", Library: "zz"}),
		}, "neither in this video's library nor here"},
		{"library item carried over from a long video", libraryOnly, map[string]string{
			"composition.html":       `<video src="assets/library-zz.webm">`,
			"assets/library-zz.webm": "clip",
			"assets/credits.json":    credits(agenttool.Credit{File: "assets/library-zz.webm", Source: "Library", Kind: "video", Library: "zz"}),
			"music.json":             `{"asset":"m9","file":"assets/library-m9.opus"}`,
			"assets/library-m9.opus": "a",
		}, ""},
		{"composed score in library mode", libraryOnly, map[string]string{"music.js": "mine"}, "music from the library only"},
		{"library score in library mode", libraryOnly, map[string]string{"music.js": "k/m2"}, ""},
		{"composed score in mixed mode", librarydomain.Policy{Music: librarydomain.ModeMixed}, map[string]string{"music.js": "mine"}, ""},
		{"two music sources", librarydomain.Policy{}, map[string]string{
			"music.js": "x", "music.json": `{"asset":"m1","file":"assets/library-m1.opus"}`, "assets/library-m1.opus": "a",
		}, "keep one music source"},
		{"track file missing", libraryOnly, map[string]string{"music.json": `{"asset":"m1","file":"assets/library-m1.opus"}`}, "missing"},
		{"track outside the directory", libraryOnly, map[string]string{"music.json": `{"asset":"m1","file":"../x.opus"}`}, "inside this directory"},
		{"track", libraryOnly, map[string]string{"music.json": `{"asset":"m1","file":"assets/library-m1.opus"}`, "assets/library-m1.opus": "a"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := workDir(t, catalog)
			for name, content := range tt.files {
				write(dir, name, content)
			}
			problems := agenttool.CheckMedia(dir, agenttool.Spec{Media: tt.policy})
			switch {
			case tt.want == "" && len(problems) > 0:
				t.Fatalf("problems = %v", problems)
			case tt.want != "" && (len(problems) != 1 || !strings.Contains(problems[0], tt.want)):
				t.Fatalf("problems = %v, want one with %q", problems, tt.want)
			}
		})
	}
}

// Stock photos are credited with what saving them to the library needs:
// the Pexels id, size, alt text and the file they came from.
func TestStockCreditsForTheLibrary(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search":
			_, _ = w.Write([]byte(`{"photos":[{"id":77,"url":"https://www.pexels.com/photo/77/","width":3000,"height":2000,
			"photographer":"Ana","photographer_url":"u","alt":"A calm sea","src":{"original":"` + srv.URL + `/o","large2x":"` + srv.URL + `/l"}}]}`))
		case "/l":
			_, _ = w.Write([]byte("jpeg"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	dir := workDir(t, nil)
	e := agenttool.Env{Dir: dir, Pexels: pexels.Client{APIKey: "key", BaseURL: srv.URL}, Spec: agenttool.Spec{Width: 1080, Height: 1920}}
	for range 2 { // the same photo twice is credited once
		if _, err := run(t, e, "stock", "--query", "sea", "--type", "photo"); err != nil {
			t.Fatal(err)
		}
	}
	credits := agenttool.ReadCredits(dir)
	if len(credits) != 1 {
		t.Fatalf("credits = %+v", credits)
	}
	c := credits[0]
	if c.Kind != "photo" || c.ExternalID != "77" || c.Alt != "A calm sea" || c.OriginalURL != srv.URL+"/o" || c.Width == 0 || c.Author != "Ana" {
		t.Fatalf("credit = %+v", c)
	}
}

func TestUsedStock(t *testing.T) {
	dir := workDir(t, nil)
	write := func(name, content string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("composition.html", `<video src="assets/pexels-video-11.webm"></video><img src="assets/pexels-photo-22.jpg"><img src="assets/library-x.jpg">`)
	b, _ := json.Marshal([]agenttool.Credit{
		{File: "assets/pexels-video-11.webm", Source: "Pexels", Kind: "video", ExternalID: "11"},
		{File: "assets/pexels-photo-22.jpg", Source: "Pexels"}, // written before credits carried kind and id
		{File: "assets/pexels-video-33.webm", Source: "Pexels", Kind: "video", ExternalID: "33"}, // downloaded, unused
		{File: "assets/library-x.jpg", Source: "Library", Kind: "photo", Library: "x"},
	})
	write("assets/credits.json", string(b))
	got := agenttool.UsedStock(dir)
	if len(got) != 2 || got[0].ExternalID != "11" || got[1].Kind != "photo" || got[1].ExternalID != "22" {
		t.Fatalf("used stock = %+v", got)
	}
}

// Stock is fetched and kept sharp enough for the final render: a 1440p
// or 2160p final takes a larger Pexels file and keeps clips that large.
func TestStockFollowsTheFinalScale(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil || testing.Short() {
		t.Skip("transcodes with ffmpeg")
	}
	src := filepath.Join(t.TempDir(), "uhd.mp4")
	if out, err := exec.Command(ffmpeg, "-y", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=3840x2160:rate=30:duration=0.2",
		"-pix_fmt", "yuv420p", src).CombinedOutput(); err != nil {
		t.Fatalf("make clip: %v: %s", err, out)
	}
	clip, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name               string
		finalScale         float64
		wantVideo, wantPic string
		wantWidth          int
	}{
		{"default is 1080p", 0, "/hd", "/l", 1920},
		{"720p keeps 1080p stock", 2.0 / 3, "/hd", "/l", 1920},
		{"1080p", 1, "/hd", "/l", 1920},
		{"1440p", 4.0 / 3, "/qhd", "/o", 2560},
		{"2160p", 2, "/uhd", "/o", 3840},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fetched []string
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/videos/search":
					_, _ = w.Write([]byte(`{"videos":[{"id":5,"url":"u","width":3840,"height":2160,"duration":10,"video_files":[
					{"link":"` + srv.URL + `/uhd","file_type":"video/mp4","width":3840,"height":2160},
					{"link":"` + srv.URL + `/hd","file_type":"video/mp4","width":1920,"height":1080},
					{"link":"` + srv.URL + `/qhd","file_type":"video/mp4","width":2560,"height":1440}]}]}`))
				case "/v1/search":
					_, _ = w.Write([]byte(`{"photos":[{"id":6,"url":"u","width":6000,"height":4000,
					"src":{"original":"` + srv.URL + `/o","large2x":"` + srv.URL + `/l"}}]}`))
				case "/hd", "/qhd", "/uhd":
					fetched = append(fetched, r.URL.Path)
					_, _ = w.Write(clip)
				case "/l", "/o":
					fetched = append(fetched, r.URL.Path)
					_, _ = w.Write([]byte("jpeg"))
				default:
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			dir := workDir(t, nil)
			e := agenttool.Env{Dir: dir, Pexels: pexels.Client{APIKey: "key", BaseURL: srv.URL},
				Spec: agenttool.Spec{Width: 1920, Height: 1080, FinalScale: tt.finalScale}}
			for _, kind := range []string{"video", "photo"} {
				if out, err := run(t, e, "stock", "--query", "sea", "--type", kind); err != nil {
					t.Fatalf("stock %s: %v: %s", kind, err, out)
				}
			}
			if want := []string{tt.wantVideo, tt.wantPic}; !slices.Equal(fetched, want) {
				t.Fatalf("fetched %v, want %v", fetched, want)
			}
			out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width",
				"-of", "csv=p=0", filepath.Join(dir, "assets", "pexels-video-5.webm")).Output()
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(out)); got != strconv.Itoa(tt.wantWidth) {
				t.Fatalf("clip width = %s, want %d", got, tt.wantWidth)
			}
		})
	}
}
