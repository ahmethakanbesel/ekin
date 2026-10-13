package render_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kurgu/internal/adapter/render"
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
	if _, err := r.Video(ctx, dir, "composition.html", 0.25, silent); err != nil {
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
	_ = os.WriteFile(filepath.Join(dir, "composition.html"), []byte(`<script src="/kit/kurgu.js"></script>
<img src="https://example.com/x.png">
<script>Kurgu.composition({duration: 1, render(t) { if (t > 0.4) throw new Error("boom") }})</script>`), 0o600)
	_, problems, err := (&render.Renderer{}).Probe(context.Background(), dir, "composition.html")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "boom") || !strings.Contains(joined, "request failed") {
		t.Fatalf("problems = %v", problems)
	}
}
