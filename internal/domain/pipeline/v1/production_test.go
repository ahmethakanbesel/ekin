package pipeline_test

import (
	"testing"

	library "kurgu/internal/domain/library/v1"
	pipeline "kurgu/internal/domain/pipeline/v1"
)

func TestNewAgent(t *testing.T) {
	tests := []struct {
		name, model, effort string
		want                pipeline.Agent
		wantErr             bool
	}{
		{name: "defaults", want: pipeline.Agent{}},
		{name: "model id and effort", model: "claude-sonnet-5-5", effort: "low", want: pipeline.Agent{Model: "claude-sonnet-5-5", Effort: pipeline.EffortLow}},
		{name: "alias with context suffix", model: " opus[1m] ", want: pipeline.Agent{Model: "opus[1m]"}},
		{name: "effort only", effort: "xhigh", want: pipeline.Agent{Effort: pipeline.EffortXHigh}},
		{name: "unknown effort", effort: "turbo", wantErr: true},
		{name: "flag injection", model: "--dangerously-skip-permissions", wantErr: true},
		{name: "spaces inside", model: "claude sonnet", wantErr: true},
		{name: "too long", model: "claude-" + string(make([]byte, 70)), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pipeline.NewAgent(tt.model, tt.effort)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPreviewQuality(t *testing.T) {
	tests := []struct {
		in        string
		wantScale float64
		wantErr   bool
	}{
		{in: "", wantScale: 0},
		{in: "360p", wantScale: 1.0 / 3},
		{in: "540p", wantScale: 0.5},
		{in: "720p", wantScale: 2.0 / 3},
		{in: "1080p", wantScale: 1},
		{in: "4k", wantErr: true},
		{in: "1440p", wantErr: true},
		{in: "1080", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			q, err := pipeline.ParsePreviewQuality(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && q.Scale() != tt.wantScale {
				t.Fatalf("scale = %v, want %v", q.Scale(), tt.wantScale)
			}
			p := pipeline.Production{PreviewQuality: pipeline.Quality(tt.in)}
			if err := p.Check(); (err != nil) != tt.wantErr {
				t.Fatalf("Check() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFinalQuality(t *testing.T) {
	tests := []struct {
		in        string
		wantScale float64
		wantErr   bool
	}{
		{in: "", wantScale: 0},
		{in: "720p", wantScale: 2.0 / 3},
		{in: "1080p", wantScale: 1},
		{in: "1440p", wantScale: 4.0 / 3},
		{in: "2160p", wantScale: 2},
		{in: "540p", wantErr: true},
		{in: "4k", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			q, err := pipeline.ParseFinalQuality(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && q.Scale() != tt.wantScale {
				t.Fatalf("scale = %v, want %v", q.Scale(), tt.wantScale)
			}
			p := pipeline.Production{FinalQuality: pipeline.Quality(tt.in)}
			if err := p.Check(); (err != nil) != tt.wantErr {
				t.Fatalf("Check() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFrameRates(t *testing.T) {
	tests := []struct {
		name           string
		preview, final int
		wantErr        bool
	}{
		{name: "defaults"},
		{name: "15 fps preview", preview: 15},
		{name: "24 fps preview", preview: 24},
		{name: "30 fps preview, 60 fps final", preview: 30, final: 60},
		{name: "30 fps final", final: 30},
		{name: "60 fps preview", preview: 60, wantErr: true},
		{name: "25 fps preview", preview: 25, wantErr: true},
		{name: "24 fps final", final: 24, wantErr: true},
		{name: "negative", final: -30, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, perr := pipeline.ParsePreviewFPS(tt.preview)
			_, ferr := pipeline.ParseFinalFPS(tt.final)
			if gotErr := perr != nil || ferr != nil; gotErr != tt.wantErr {
				t.Fatalf("parse errors = %v, %v; wantErr %v", perr, ferr, tt.wantErr)
			}
			p := pipeline.Production{PreviewFPS: pipeline.FrameRate(tt.preview), FinalFPS: pipeline.FrameRate(tt.final)}
			if err := p.Check(); (err != nil) != tt.wantErr {
				t.Fatalf("Check() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProductionChecksMedia(t *testing.T) {
	tests := []struct {
		name    string
		media   library.Policy
		wantErr bool
	}{
		{name: "all fresh", media: library.Policy{}},
		{name: "library music, mixed video", media: library.Policy{Music: library.ModeLibrary, Video: library.ModeMixed}},
		{name: "unknown mode", media: library.Policy{Photo: "anything"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pipeline.Production{Media: tt.media}.Check()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestThumbnailCount(t *testing.T) {
	n := func(v int) *int { return &v }
	tests := []struct {
		name    string
		in      *int
		want    pipeline.ThumbnailCount
		count   int
		wire    *int
		wantErr bool
	}{
		{name: "absent is the default", in: nil, want: 0, count: 3, wire: nil},
		{name: "zero turns them off", in: n(0), want: pipeline.ThumbnailsOff, count: 0, wire: n(0)},
		{name: "one", in: n(1), want: 1, count: 1, wire: n(1)},
		{name: "four", in: n(4), want: 4, count: 4, wire: n(4)},
		{name: "five", in: n(5), wantErr: true},
		{name: "negative", in: n(-1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pipeline.ParseThumbnailCount(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseThumbnailCount() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want || got.Count() != tt.count {
				t.Fatalf("ParseThumbnailCount() = %d (count %d), want %d (count %d)", got, got.Count(), tt.want, tt.count)
			}
			if w := got.Wire(); (w == nil) != (tt.wire == nil) || (w != nil && *w != *tt.wire) {
				t.Fatalf("Wire() = %v, want %v", w, tt.wire)
			}
			if err := (pipeline.Production{Thumbnails: got}).Check(); err != nil {
				t.Fatalf("Check() = %v", err)
			}
		})
	}
	if err := (pipeline.Production{Thumbnails: 5}).Check(); err == nil {
		t.Fatal("Check() accepted 5 thumbnail variants")
	}
}
