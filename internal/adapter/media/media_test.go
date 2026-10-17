package media_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"kurgu/internal/adapter/media"
	librarydomain "kurgu/internal/domain/library/v1"
)

func need(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip("no " + bin)
		}
	}
}

func generate(t *testing.T, name string, args ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("ffmpeg", append(append([]string{"-y", "-loglevel", "error"}, args...), out)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate %s: %v: %s", name, err, b)
	}
	return out
}

func TestIngest(t *testing.T) {
	need(t)
	ctx := context.Background()
	p := &media.Processor{}
	tests := []struct {
		name   string
		kind   librarydomain.Kind
		in     string
		want   librarydomain.Media
		poster bool
	}{
		{
			name: "portrait video is scaled to 1920 and loses its sound",
			kind: librarydomain.KindVideo,
			in: generate(t, "clip.mp4", "-f", "lavfi", "-i", "testsrc2=size=1440x2560:rate=25:duration=2",
				"-f", "lavfi", "-i", "sine=duration=2", "-shortest", "-pix_fmt", "yuv420p"),
			want: librarydomain.Media{Format: "webm", Width: 1080, Height: 1920, Poster: true},
		},
		{
			name: "small photo keeps its size",
			kind: librarydomain.KindPhoto,
			in:   generate(t, "photo.png", "-f", "lavfi", "-i", "testsrc2=size=800x600", "-frames:v", "1"),
			want: librarydomain.Media{Format: "jpg", Width: 800, Height: 600, Poster: true},
		},
		{
			name: "large photo is bounded",
			kind: librarydomain.KindPhoto,
			in:   generate(t, "big.jpg", "-f", "lavfi", "-i", "testsrc2=size=4320x2160", "-frames:v", "1"),
			want: librarydomain.Media{Format: "jpg", Width: 2160, Height: 1080, Poster: true},
		},
		{
			name: "music becomes opus",
			kind: librarydomain.KindMusic,
			in:   generate(t, "track.wav", "-f", "lavfi", "-i", "sine=frequency=440:duration=3"),
			want: librarydomain.Media{Format: "opus"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			got, err := p.Ingest(ctx, tt.kind, tt.in, dir)
			if err != nil {
				t.Fatal(err)
			}
			if got.Format != tt.want.Format || got.Width != tt.want.Width || got.Height != tt.want.Height || got.Poster != tt.want.Poster {
				t.Fatalf("media = %+v, want %+v", got, tt.want)
			}
			if tt.kind != librarydomain.KindPhoto && (got.Duration < 1500*time.Millisecond || got.Duration > 3500*time.Millisecond) {
				t.Fatalf("duration = %v", got.Duration)
			}
			if _, err := os.Stat(filepath.Join(dir, "main."+got.Format)); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "poster.jpg")); (err == nil) != got.Poster {
				t.Fatalf("poster on disk = %v, reported %v", err == nil, got.Poster)
			}
			if tt.kind == librarydomain.KindVideo {
				info, _ := p.Probe(ctx, filepath.Join(dir, "main.webm"))
				if info.Audio {
					t.Fatal("video kept its sound")
				}
			}
		})
	}
}

func TestIngestRejects(t *testing.T) {
	need(t)
	ctx := context.Background()
	p := &media.Processor{}
	junk := filepath.Join(t.TempDir(), "junk.mp4")
	if err := os.WriteFile(junk, []byte("not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Ingest(ctx, librarydomain.KindVideo, junk, t.TempDir()); !errors.Is(err, media.ErrUnreadable) {
		t.Fatalf("junk err = %v", err)
	}
	silent := generate(t, "silent.mp4", "-f", "lavfi", "-i", "testsrc2=size=320x240:duration=1", "-pix_fmt", "yuv420p")
	if _, err := p.Ingest(ctx, librarydomain.KindMusic, silent, t.TempDir()); !errors.Is(err, media.ErrUnreadable) {
		t.Fatalf("music without sound err = %v", err)
	}
}

func TestInspectAndPreview(t *testing.T) {
	need(t)
	ctx := context.Background()
	p := &media.Processor{}
	clip := generate(t, "clip.webm", "-f", "lavfi", "-i", "testsrc2=size=540x960:rate=30:duration=3", "-c:v", "libvpx")
	dir := t.TempDir()
	m, err := p.Inspect(ctx, librarydomain.KindVideo, clip, dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != "webm" || m.Width != 540 || m.Height != 960 || !m.Poster || m.Duration < 2*time.Second {
		t.Fatalf("inspected = %+v", m)
	}
	if _, err := os.Stat(filepath.Join(dir, "poster.jpg")); err != nil {
		t.Fatal(err)
	}
	photo := generate(t, "photo.jpg", "-f", "lavfi", "-i", "testsrc2=size=800x600", "-frames:v", "1")
	if m, err := p.Inspect(ctx, librarydomain.KindPhoto, photo, t.TempDir()); err != nil || m.Format != "jpg" || m.Duration != 0 {
		t.Fatalf("photo = %+v, %v", m, err)
	}
	wav := generate(t, "score.wav", "-f", "lavfi", "-i", "sine=frequency=440:duration=40")
	out := filepath.Join(t.TempDir(), "preview.opus")
	if err := p.Preview(ctx, wav, out); err != nil {
		t.Fatal(err)
	}
	info, err := p.Probe(ctx, out)
	if err != nil || !info.Audio || info.Duration > 31*time.Second {
		t.Fatalf("preview = %+v, %v", info, err)
	}
}
