package render_test

import (
	"context"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/render"
)

func needTools(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("renders in Chrome")
	}
	if render.FindChrome() == "" {
		t.Skip("no Chrome")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
}

func probeDuration(t *testing.T, path string) float64 {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return d
}

func TestRenderPipeline(t *testing.T) {
	needTools(t)
	ctx := context.Background()
	dir, _ := filepath.Abs("testdata/comp")
	out := t.TempDir()
	r := &render.Renderer{Chunks: 2}

	m, problems, err := r.Probe(ctx, dir, "composition.html")
	if err != nil || len(problems) > 0 {
		t.Fatalf("probe = %+v %v %v", m, problems, err)
	}
	if m.Duration != 2 || m.FPS != 30 || m.Width != 1080 {
		t.Fatalf("meta = %+v", m)
	}

	frames, _, err := r.Snapshot(ctx, dir, "composition.html", []float64{0.6}, 0.5, out)
	if err != nil || len(frames) != 1 {
		t.Fatalf("snapshot = %v %v", frames, err)
	}

	silent := filepath.Join(out, "silent.mp4")
	if _, err := r.Video(ctx, dir, "composition.html", render.VideoOptions{Scale: 0.25}, silent); err != nil {
		t.Fatal(err)
	}
	if d := probeDuration(t, silent); d < 1.9 || d > 2.1 {
		t.Fatalf("video duration = %.2f", d)
	}

	music := filepath.Join(out, "music.wav")
	peak, err := r.Music(ctx, dir, 2, music)
	if err != nil || peak <= 0 {
		t.Fatalf("music = %v %v", peak, err)
	}
	voice := filepath.Join(out, "voice.wav")
	if err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=220:duration=1.5", voice).Run(); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(out, "final.mp4")
	if err := r.Mix(ctx, silent, voice, music, 2, final); err != nil {
		t.Fatal(err)
	}
	if d := probeDuration(t, final); d < 1.9 || d > 2.1 {
		t.Fatalf("mixed duration = %.2f", d)
	}
	streams, _ := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", final).Output()
	if !strings.Contains(string(streams), "audio") {
		t.Fatalf("no audio stream: %s", streams)
	}
	if err := r.Poster(ctx, final, 1, filepath.Join(out, "poster.jpg")); err != nil {
		t.Fatal(err)
	}
}

func TestProbeReportsBrokenPage(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "composition.html"), []byte(`<script src="/kit/ekin.js"></script>
<img src="https://example.com/x.png">
<script>Ekin.composition({duration: 1, render(t) { if (t > 0.4) throw new Error("boom") }})</script>`), 0o600)
	_, problems, err := (&render.Renderer{}).Probe(context.Background(), dir, "composition.html")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "boom") || !strings.Contains(joined, "request failed") {
		t.Fatalf("problems = %v", problems)
	}
}

func TestMusicTrack(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip("no " + bin)
		}
	}
	dir := t.TempDir()
	track := filepath.Join(dir, "track.mp3")
	if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=220:duration=3", track).CombinedOutput(); err != nil {
		t.Fatalf("make track: %v %s", err, out)
	}
	r := &render.Renderer{}
	tests := []struct {
		name             string
		offset, duration float64
	}{
		{"looped when shorter", 0, 7.5},
		{"from an offset", 1, 1.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "-")+".wav")
			if err := r.MusicTrack(context.Background(), track, tt.offset, tt.duration, out); err != nil {
				t.Fatal(err)
			}
			raw, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", out).Output()
			if err != nil {
				t.Fatal(err)
			}
			got, _ := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
			if math.Abs(got-tt.duration) > 0.05 {
				t.Fatalf("duration = %.3f, want %.3f", got, tt.duration)
			}
		})
	}
}

func TestThumbnails(t *testing.T) {
	needTools(t)
	dir, _ := filepath.Abs("testdata/thumb")
	out := t.TempDir()
	shots, m, problems, err := (&render.Renderer{}).Thumbnails(context.Background(), dir, "thumbnail.html", []int{1, 2}, true, out)
	if err != nil || len(problems) > 0 {
		t.Fatalf("thumbnails = %v, problems %v", err, problems)
	}
	if m.Width != 1280 || m.Height != 720 || len(shots) != 2 {
		t.Fatalf("meta = %+v, shots = %+v", m, shots)
	}
	for _, s := range shots {
		if w, h := jpegSize(t, s.Path); w != 1280 || h != 720 || s.Bytes == 0 || s.Bytes > render.MaxThumbnailBytes {
			t.Errorf("thumbnail %d is %dx%d, %d bytes", s.N, w, h, s.Bytes)
		}
		if w, h := jpegSize(t, s.Small); w != render.FeedWidth || h != 180 {
			t.Errorf("small thumbnail %d is %dx%d", s.N, w, h)
		}
	}
	if !strings.Contains(shots[0].Text, "Kaygı mı?") || len(shots[0].Corner) != 0 {
		t.Errorf("variant 1 = %q, corner %v", shots[0].Text, shots[0].Corner)
	}
	if !strings.Contains(shots[1].Text, "Köşede yazı") || len(shots[1].Corner) != 1 {
		t.Errorf("variant 2 = %q, corner %v", shots[1].Text, shots[1].Corner)
	}

	// A composition is not a thumbnail page.
	comp, _ := filepath.Abs("testdata/comp")
	if _, _, _, err := (&render.Renderer{}).Thumbnails(context.Background(), comp, "composition.html", []int{1}, false, out); err == nil {
		t.Fatal("a composition rendered as a thumbnail page")
	}
}

func jpegSize(t *testing.T, path string) (int, int) {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // test output
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	c, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	return c.Width, c.Height
}

func TestTranscodeBitrate(t *testing.T) {
	for _, tt := range []struct {
		maxSide int
		want    string
	}{{1080, "3M"}, {1920, "3M"}, {2560, "5M"}, {3840, "12M"}} {
		if got := render.TranscodeBitrate(tt.maxSide); got != tt.want {
			t.Errorf("TranscodeBitrate(%d) = %s, want %s", tt.maxSide, got, tt.want)
		}
	}
}
