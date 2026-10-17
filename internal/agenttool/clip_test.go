package agenttool_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kurgu/internal/adapter/render"
	"kurgu/internal/agenttool"
)

// prober stands in for Chrome: every composition loads with meta.
type prober struct{ meta render.Meta }

func (p prober) Probe(context.Context, string, string) (render.Meta, []string, error) {
	return p.meta, nil, nil
}

func TestTTSRefusedForAClip(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "narration"), 0o750)
	_ = os.WriteFile(filepath.Join(dir, "narration", "narration.txt"), []byte("Bir cümle."), 0o600)
	var out bytes.Buffer
	env := agenttool.Env{Dir: dir, Stdout: &out, Stderr: &out, Spec: agenttool.Spec{Language: "tr", Clip: true}}
	err := agenttool.Run(context.Background(), env, []string{"tts"})
	if err == nil || !strings.Contains(err.Error(), "long video's narration") {
		t.Fatalf("tts for a clip = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "narration", "speech.wav")); err == nil {
		t.Fatal("tts wrote narration for a clip")
	}
}

func TestCheckKeepsAClipsNarration(t *testing.T) {
	tests := []struct {
		name      string
		narration string // timings.json duration
		wantErr   string
	}{
		{"as prepared", "20.4", ""},
		{"within rounding", "20.45", ""},
		{"changed", "15", "not the clip's 20.40 s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			_ = os.MkdirAll(filepath.Join(dir, "narration"), 0o750)
			_ = os.WriteFile(filepath.Join(dir, "composition.html"), []byte(`<script src="/kit/kurgu.js"></script>`), 0o600)
			_ = os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"title":"Öfke","description":"d","tags":[]}`), 0o600)
			_ = os.WriteFile(filepath.Join(dir, "narration", "timings.json"), []byte(`{"duration": `+tt.narration+`, "words": []}`), 0o600)
			spec := agenttool.Spec{Language: "tr", Width: 1080, Height: 1920, MinDuration: 20.4, MaxDuration: 24.4, Clip: true}
			rep := agenttool.Check(context.Background(), prober{render.Meta{Width: 1080, Height: 1920, FPS: 30, Duration: 21}}, dir, "composition.html", spec)
			errs := strings.Join(rep.Errors, "\n")
			if tt.wantErr == "" && !rep.OK {
				t.Fatalf("check failed: %s", errs)
			}
			if tt.wantErr != "" && (rep.OK || !strings.Contains(errs, tt.wantErr)) {
				t.Fatalf("errors = %s, want %q", errs, tt.wantErr)
			}
		})
	}
}
