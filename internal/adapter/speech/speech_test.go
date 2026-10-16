package speech_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"kurgu/internal/adapter/speech"
)

// fakeUV stands in for `uv run --script`: it writes the timings.json a
// voice script would, with a field Kurgu does not know.
const fakeUV = `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in --out-dir) out="$2"; shift ;; esac
  shift
done
printf '{"engine":"script","sampleRate":24000,"duration":1.5,"words":[{"text":"Hi","start":0,"end":0.5}],"segments":[],"extra":7}' > "$out/timings.json"
`

// A local voice's timings.json says which voice read it, and keeps what
// the script wrote.
func TestSayRecordsTheVoice(t *testing.T) {
	dir := t.TempDir()
	uv := filepath.Join(dir, "uv")
	if err := os.WriteFile(uv, []byte(fakeUV), 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}
	in := filepath.Join(dir, "in.txt")
	_ = os.WriteFile(in, []byte("Hi."), 0o600)
	s := &speech.Synth{UV: uv, ScriptDir: dir}
	tests := []struct {
		name, lang string
		o          speech.Options
		want       string
	}{
		{"chosen english voice", "en", speech.Options{Voice: "bm_george"}, "bm_george"},
		{"english default", "en", speech.Options{}, ""},
		{"turkish engine's only voice", "tr", speech.Options{Engine: speech.EngineEMA, Voice: "ema"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(dir, tt.name)
			_ = os.MkdirAll(out, 0o750)
			tm, err := s.Say(context.Background(), tt.lang, in, out, tt.o)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(filepath.Join(out, "timings.json"))
			var saved map[string]any
			if err := json.Unmarshal(raw, &saved); err != nil {
				t.Fatal(err)
			}
			voice, _ := saved["voice"].(string)
			if tm.Voice != tt.want || voice != tt.want || saved["extra"] != float64(7) || saved["engine"] != "script" {
				t.Fatalf("returned voice %q, timings.json %s", tm.Voice, raw)
			}
			if _, ok := saved["model"]; ok {
				t.Fatalf("a local voice has no model: %s", raw)
			}
		})
	}
}
