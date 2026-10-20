package speech_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/elevenlabs"
	"github.com/ahmethakanbesel/ekin/internal/adapter/speech"
)

// The fake's sample rate, lead-in and seconds per character. Package
// variables, not constants: gosec 2.23 panics on float constants in
// integer conversions.
var rate, lead, per = 24000.0, 0.2, 0.05

// fakeElevenLabs speaks every character for 50 ms as a tone (spaces are
// silent), after 200 ms of silence, and records the requests.
func fakeElevenLabs(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var (
		mu   sync.Mutex
		reqs []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("xi-api-key") != "sk_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"detail":{"status":"invalid_api_key","message":"Invalid API key"}}`))
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/text-to-speech/voice123456/with-timestamps") || r.URL.Query().Get("output_format") != "pcm_24000" {
			t.Errorf("unexpected request %s", r.URL)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		reqs = append(reqs, body)
		mu.Unlock()
		text := []rune(body["text"].(string))
		pcm := make([]byte, 2*int(rate*(lead+per*float64(len(text))+0.2)))
		chars, starts, ends := []string{}, []float64{}, []float64{}
		for i, ch := range text {
			s := lead + per*float64(i)
			chars, starts, ends = append(chars, string(ch)), append(starts, s), append(ends, s+per)
			if ch == ' ' {
				continue
			}
			for k := int(s * rate); k < int((s+per)*rate); k++ {
				binary.LittleEndian.PutUint16(pcm[2*k:], uint16(k%100*200)) //nolint:gosec // a sawtooth test tone
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"audio_base64": base64.StdEncoding.EncodeToString(pcm),
			"alignment":    map[string]any{"characters": chars, "character_start_times_seconds": starts, "character_end_times_seconds": ends},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func TestElevenLabsNarration(t *testing.T) {
	srv, reqs := fakeElevenLabs(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(in, []byte("Hi acme, ok.\n\nBye — now!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &speech.Synth{ElevenLabsURL: srv.URL, ElevenLabsKey: func(context.Context) string { return "sk_test" }}
	tm, err := s.Say(context.Background(), "tr", in, dir, speech.Options{
		Engine: speech.EngineElevenLabs, Voice: "voice123456", Speed: 1.1, Seed: 9, ParagraphPause: 0.5,
		Lexicon:    map[string]string{"acme": "ek mi"},
		ElevenLabs: &speech.ElevenLabsOptions{Model: "eleven_flash_v2_5", Stability: 0.4, SimilarityBoost: 0.8, SpeakerBoost: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(*reqs) != 2 {
		t.Fatalf("requests = %d, want one per paragraph", len(*reqs))
	}
	first, second := (*reqs)[0], (*reqs)[1]
	if first["text"] != "Hi ek mi, ok." || first["next_text"] != "Bye — now!" || second["previous_text"] != "Hi ek mi, ok." {
		t.Fatalf("requests = %v", *reqs)
	}
	if first["model_id"] != "eleven_flash_v2_5" || first["language_code"] != "tr" || first["seed"] != float64(9) {
		t.Fatalf("first request = %v", first)
	}
	if vs := first["voice_settings"].(map[string]any); vs["speed"] != 1.1 || vs["stability"] != 0.4 || vs["use_speaker_boost"] != true {
		t.Fatalf("voice settings = %v", vs)
	}

	// Captions keep the written words; the dash has no letters but is
	// still spoken time, so it keeps its own span.
	var got []string
	for _, w := range tm.Words {
		got = append(got, w.Text)
	}
	if strings.Join(got, "|") != "Hi|acme,|ok.|Bye|—|now!" {
		t.Fatalf("words = %v", got)
	}
	// Leading silence is trimmed to 30 ms: "Hi" starts at 0.03 s and the
	// respelled "ek mi," spans both spoken words up to its last letter
	// (5 characters; the comma is left out).
	if w := tm.Words[0]; math.Abs(w.Start-0.03) > 0.011 || math.Abs(w.End-(0.03+0.1)) > 0.011 {
		t.Fatalf("Hi = %+v", w)
	}
	if w := tm.Words[1]; math.Abs((w.End-w.Start)-0.25) > 0.011 {
		t.Fatalf("acme = %+v, want 5 characters long", w)
	}
	// The second paragraph starts after the first one's sound plus the
	// paragraph pause.
	if seg := tm.Segments; len(seg) != 2 || math.Abs(seg[1].Start-(seg[0].End+0.5)) > 0.001 {
		t.Fatalf("segments = %+v", seg)
	}
	for i := 1; i < len(tm.Words); i++ {
		if tm.Words[i].Start < tm.Words[i-1].End || tm.Words[i].End > tm.Duration {
			t.Fatalf("word %d out of order: %+v", i, tm.Words)
		}
	}

	wav, err := os.ReadFile(filepath.Join(dir, "speech.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if string(wav[:4]) != "RIFF" || binary.LittleEndian.Uint32(wav[24:]) != elevenlabs.SampleRate {
		t.Fatal("speech.wav is not a 24 kHz WAV")
	}
	if secs := float64(len(wav)-44) / 2 / elevenlabs.SampleRate; math.Abs(secs-tm.Duration) > 0.001 {
		t.Fatalf("wav %v s, timings %v s", secs, tm.Duration)
	}
	var saved speech.Timings
	raw, _ := os.ReadFile(filepath.Join(dir, "timings.json"))
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Engine != "elevenlabs" || len(saved.Words) != len(tm.Words) ||
		saved.Voice != "voice123456" || saved.Model != "eleven_flash_v2_5" {
		t.Fatalf("timings.json = %s (%v)", raw, err)
	}
}

func TestElevenLabsErrors(t *testing.T) {
	srv, _ := fakeElevenLabs(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.txt")
	_ = os.WriteFile(in, []byte("Merhaba."), 0o600)
	o := speech.Options{Engine: speech.EngineElevenLabs, Voice: "voice123456"}

	if _, err := (&speech.Synth{ElevenLabsURL: srv.URL}).Say(context.Background(), "tr", in, dir, o); err == nil ||
		!strings.Contains(err.Error(), "API key") {
		t.Fatalf("no key: %v", err)
	}
	bad := &speech.Synth{ElevenLabsURL: srv.URL, ElevenLabsKey: func(context.Context) string { return "sk_wrong" }}
	if _, err := bad.Say(context.Background(), "tr", in, dir, o); err == nil || !strings.Contains(err.Error(), "invalid_api_key") {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := (&speech.Synth{}).Say(context.Background(), "en", in, dir, speech.Options{Engine: speech.EngineEMA}); err == nil {
		t.Fatal("EMA was accepted for English")
	}
}

func TestElevenLabsLongParagraph(t *testing.T) {
	srv, reqs := fakeElevenLabs(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.txt")
	sentence := strings.Repeat("kelime ", 59) + "son."
	_ = os.WriteFile(in, []byte(strings.Repeat(sentence+" ", 12)), 0o600) // ~4,800 characters, one paragraph
	s := &speech.Synth{ElevenLabsURL: srv.URL, ElevenLabsKey: func(context.Context) string { return "sk_test" }}
	tm, err := s.Say(context.Background(), "tr", in, dir, speech.Options{Engine: speech.EngineElevenLabs, Voice: "voice123456"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*reqs) != 3 { // 418 characters a sentence: five fit in 2,500
		t.Fatalf("requests = %d, want 3", len(*reqs))
	}
	for _, r := range *reqs {
		text := r["text"].(string)
		if len([]rune(text)) > 2500 || !strings.HasSuffix(text, "son.") {
			t.Fatalf("chunk of %d characters ends %q", len([]rune(text)), text[len(text)-10:])
		}
	}
	if len(tm.Words) != 12*60 {
		t.Fatalf("words = %d", len(tm.Words))
	}
}
