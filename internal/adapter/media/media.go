// Package media turns uploaded files into what the library stores, with
// ffmpeg: videos become silent, seekable VP8 WebM (as stock clips do),
// photos a bounded JPEG, music loudness-normalised Opus.
package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	librarydomain "kurgu/internal/domain/library/v1"
)

// Bounds of what ingestion produces.
const (
	maxVideoSide  = 1920
	maxPhotoSide  = 2160
	maxPosterSide = 480
	// musicLoudness is where uploaded tracks are normalised (LUFS), near
	// the level of the scores the agent composes, so the mix's fixed music
	// volume suits both.
	musicLoudness = -16
)

// Processor runs ffmpeg and ffprobe.
type Processor struct {
	FFmpeg  string // default "ffmpeg"
	FFprobe string // default: ffprobe beside FFmpeg, else "ffprobe"
	Threads int    // 0 lets ffmpeg choose
}

// ErrUnreadable means the file is not a picture or sound ffmpeg can read.
var ErrUnreadable = errors.New("the file could not be read as media")

// Info is what ffprobe reports about a file.
type Info struct {
	Width, Height int
	Duration      time.Duration
	Video, Audio  bool
}

// Ingest converts in into outDir/main.<format> (and outDir/poster.jpg
// for pictures) and reports what it made.
func (p *Processor) Ingest(ctx context.Context, kind librarydomain.Kind, in, outDir string) (librarydomain.Media, error) {
	info, err := p.Probe(ctx, in)
	if err != nil {
		return librarydomain.Media{}, err
	}
	switch kind {
	case librarydomain.KindVideo:
		return p.video(ctx, in, outDir, info)
	case librarydomain.KindPhoto:
		return p.photo(ctx, in, outDir, info)
	case librarydomain.KindMusic:
		return p.music(ctx, in, outDir, info)
	}
	return librarydomain.Media{}, fmt.Errorf("media: unknown kind %q", kind)
}

func (p *Processor) video(ctx context.Context, in, outDir string, info Info) (librarydomain.Media, error) {
	if !info.Video || info.Duration <= 0 {
		return librarydomain.Media{}, fmt.Errorf("%w: no video stream", ErrUnreadable)
	}
	if info.Duration > librarydomain.MaxVideoDuration+time.Second {
		return librarydomain.Media{Format: "webm", Width: info.Width, Height: info.Height, Duration: info.Duration}, nil
	}
	main := filepath.Join(outDir, "main.webm")
	err := p.run(ctx, "-i", in, "-an", "-sn", "-map_metadata", "-1",
		"-vf", fitLongSide(maxVideoSide)+",fps=30", "-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8",
		"-b:v", "3M", "-g", "15", main)
	if err != nil {
		return librarydomain.Media{}, err
	}
	out, err := p.Probe(ctx, main)
	if err != nil {
		return librarydomain.Media{}, err
	}
	at := math.Min(1, out.Duration.Seconds()/3)
	if err := p.run(ctx, "-ss", strconv.FormatFloat(at, 'f', 2, 64), "-i", main, "-frames:v", "1",
		"-vf", fitLongSide(maxPosterSide), "-q:v", "4", filepath.Join(outDir, "poster.jpg")); err != nil {
		return librarydomain.Media{}, err
	}
	return librarydomain.Media{Format: "webm", Width: out.Width, Height: out.Height, Duration: out.Duration, Poster: true}, nil
}

func (p *Processor) photo(ctx context.Context, in, outDir string, info Info) (librarydomain.Media, error) {
	if !info.Video || info.Width <= 0 || info.Height <= 0 {
		return librarydomain.Media{}, fmt.Errorf("%w: no picture", ErrUnreadable)
	}
	main := filepath.Join(outDir, "main.jpg")
	if err := p.run(ctx, "-i", in, "-frames:v", "1", "-map_metadata", "-1",
		"-vf", fitLongSide(maxPhotoSide), "-q:v", "2", main); err != nil {
		return librarydomain.Media{}, err
	}
	out, err := p.Probe(ctx, main)
	if err != nil {
		return librarydomain.Media{}, err
	}
	if err := p.run(ctx, "-i", main, "-vf", fitLongSide(maxPosterSide), "-q:v", "4",
		filepath.Join(outDir, "poster.jpg")); err != nil {
		return librarydomain.Media{}, err
	}
	return librarydomain.Media{Format: "jpg", Width: out.Width, Height: out.Height, Poster: true}, nil
}

func (p *Processor) music(ctx context.Context, in, outDir string, info Info) (librarydomain.Media, error) {
	if !info.Audio || info.Duration <= 0 {
		return librarydomain.Media{}, fmt.Errorf("%w: no sound", ErrUnreadable)
	}
	if info.Duration > librarydomain.MaxMusicDuration+time.Second {
		return librarydomain.Media{Format: "opus", Duration: info.Duration}, nil
	}
	main := filepath.Join(outDir, "main.opus")
	if err := p.run(ctx, "-i", in, "-vn", "-sn", "-map_metadata", "-1",
		"-af", fmt.Sprintf("loudnorm=I=%d:TP=-1.5:LRA=11", musicLoudness),
		"-ar", "48000", "-c:a", "libopus", "-b:a", "128k", main); err != nil {
		return librarydomain.Media{}, err
	}
	out, err := p.Probe(ctx, main)
	if err != nil {
		return librarydomain.Media{}, err
	}
	return librarydomain.Media{Format: "opus", Duration: out.Duration}, nil
}

// Inspect reports on a file already in its stored format (a draft's
// transcoded clip or photo) and writes its poster to outDir/poster.jpg.
func (p *Processor) Inspect(ctx context.Context, kind librarydomain.Kind, in, outDir string) (librarydomain.Media, error) {
	info, err := p.Probe(ctx, in)
	if err != nil {
		return librarydomain.Media{}, err
	}
	if !info.Video || info.Width <= 0 {
		return librarydomain.Media{}, fmt.Errorf("%w: no picture", ErrUnreadable)
	}
	m := librarydomain.Media{Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(in)), "."), Width: info.Width,
		Height: info.Height, Poster: true}
	at := 0.0
	if kind == librarydomain.KindVideo {
		m.Duration = info.Duration
		at = math.Min(1, info.Duration.Seconds()/3)
	}
	if err := p.run(ctx, "-ss", strconv.FormatFloat(at, 'f', 2, 64), "-i", in, "-frames:v", "1",
		"-vf", fitLongSide(maxPosterSide), "-q:v", "4", filepath.Join(outDir, "poster.jpg")); err != nil {
		return librarydomain.Media{}, err
	}
	return m, nil
}

// previewSeconds is how much of a composed score its preview holds.
const previewSeconds = 30

// Preview encodes the start of a rendered score (a WAV) as Opus, for
// listening in the library.
func (p *Processor) Preview(ctx context.Context, in, out string) error {
	return p.run(ctx, "-i", in, "-t", strconv.Itoa(previewSeconds), "-vn", "-ar", "48000", "-c:a", "libopus", "-b:a", "96k", out)
}

// fitLongSide scales a picture down so its long side is at most n
// pixels, keeping even dimensions; smaller pictures are left alone.
func fitLongSide(n int) string {
	return fmt.Sprintf("scale='if(gte(iw,ih),min(%d,iw),-2)':'if(gte(iw,ih),-2,min(%d,ih))'", n, n)
}

// Probe reads a file's picture size, length and streams.
func (p *Processor) Probe(ctx context.Context, path string) (Info, error) {
	cmd := exec.CommandContext(ctx, p.ffprobe(), "-v", "error", "-of", "json", //nolint:gosec // fixed binary
		"-show_entries", "stream=codec_type,width,height,duration:stream_disposition=attached_pic:format=duration", path)
	raw, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Info{}, errors.New("media: ffprobe is not installed")
		}
		if ctx.Err() != nil {
			return Info{}, ctx.Err()
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return Info{}, fmt.Errorf("%w: %s", ErrUnreadable, firstLine(string(ee.Stderr)))
		}
		return Info{}, fmt.Errorf("media: ffprobe: %w", err)
	}
	var out struct {
		Streams []struct {
			CodecType   string `json:"codec_type"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
			Duration    string `json:"duration"`
			Disposition struct {
				AttachedPic int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Info{}, fmt.Errorf("media: ffprobe output: %w", err)
	}
	var info Info
	for _, s := range out.Streams {
		switch {
		case s.CodecType == "video" && s.Disposition.AttachedPic == 0 && !info.Video:
			info.Video, info.Width, info.Height = true, s.Width, s.Height
		case s.CodecType == "audio":
			info.Audio = true
		}
	}
	if secs, err := strconv.ParseFloat(out.Format.Duration, 64); err == nil && secs > 0 {
		info.Duration = time.Duration(secs * float64(time.Second))
	}
	return info, nil
}

func (p *Processor) ffmpeg() string {
	if p.FFmpeg != "" {
		return p.FFmpeg
	}
	return "ffmpeg"
}

func (p *Processor) ffprobe() string {
	if p.FFprobe != "" {
		return p.FFprobe
	}
	if dir := filepath.Dir(p.ffmpeg()); dir != "." {
		return filepath.Join(dir, "ffprobe")
	}
	return "ffprobe"
}

func (p *Processor) run(ctx context.Context, args ...string) error {
	base := []string{"-y", "-loglevel", "error", "-nostdin"}
	if p.Threads > 0 {
		base = append(base, "-threads", strconv.Itoa(p.Threads))
	}
	cmd := exec.CommandContext(ctx, p.ffmpeg(), append(base, args...)...) //nolint:gosec // fixed binary
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("media: ffmpeg is not installed")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %s", ErrUnreadable, firstLine(string(out)))
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
