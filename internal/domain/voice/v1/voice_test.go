package voice_test

import (
	"testing"

	"kurgu/internal/apperror"
	voice "kurgu/internal/domain/voice/v1"
)

func TestSettings(t *testing.T) {
	s := voice.Settings{Languages: map[string]voice.Config{"en": {Voice: "bm_george", Speed: 1.2}}}.Normalize()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if c := s.For("tr", nil); c.Voice != "okur" || c.Speed != 1 {
		t.Fatalf("tr default = %+v", c)
	}
	if c := s.For("en", &voice.Override{Speed: 0.9}); c.Voice != "bm_george" || c.Speed != 0.9 || c.Quality != "fp32" {
		t.Fatalf("en override = %+v", c)
	}
	bad := []voice.Settings{
		{Languages: map[string]voice.Config{"en": {Voice: "zf_xiaoxiao", Speed: 1}}},
		{Languages: map[string]voice.Config{"tr": {Voice: "okur", Speed: 3}}},
		{Languages: map[string]voice.Config{"tr": {Voice: "okur", Speed: 1, ParagraphPause: 5}}},
		{Languages: map[string]voice.Config{"en": {Voice: "af_heart", Speed: 1, Quality: "fp64"}}},
		{Lexicon: map[string]map[string]string{"tr": {"two words": "x"}}},
		{Lexicon: map[string]map[string]string{"de": {"a": "b"}}},
	}
	for i, b := range bad {
		if err := b.Validate(); !apperror.Is(err, apperror.ValidationFailed) {
			t.Errorf("case %d accepted: %v", i, err)
		}
	}
	if err := (voice.Override{Voice: "af_heart"}).Validate("tr"); err == nil {
		t.Fatal("an English voice was accepted for Turkish")
	}
	if e, _ := voice.EngineFor("en", ""); e.ID != voice.EngineKokoro || len(e.Voices) != 28 {
		t.Fatalf("en default engine = %s with %d voices", e.ID, len(e.Voices))
	}
}

func TestEngines(t *testing.T) {
	// Settings saved before engines were selectable keep their engine.
	old := voice.Settings{Languages: map[string]voice.Config{"tr": {Voice: "okur", Speed: 1.1, Seed: 7}}}.Normalize()
	if c := old.Languages["tr"]; c.Engine != voice.EngineOkur || c.Seed != 7 || c.ElevenLabs != nil {
		t.Fatalf("old tr = %+v", c)
	}

	s := voice.Settings{Languages: map[string]voice.Config{
		"tr": {Engine: voice.EngineEMA, Speed: 1, SentencePause: 0.3, ParagraphPause: 0.6, Quality: "int8"},
		"en": {Engine: voice.EngineElevenLabs, Voice: "21m00Tcm4TlvDq8ikWAM", Speed: 1.1, ElevenLabs: &voice.ElevenLabs{Stability: 0.4}},
	}}.Normalize()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if c := s.Languages["tr"]; c.Voice != "ema" || c.Quality != "" {
		t.Fatalf("ema = %+v", c)
	}
	if el := s.Languages["en"].ElevenLabs; el == nil || el.Model != "eleven_multilingual_v2" || el.Stability != 0.4 {
		t.Fatalf("elevenlabs = %+v", el)
	}

	tests := []struct {
		name string
		lang string
		o    voice.Override
		want voice.Config
	}{
		{"speed only keeps the engine", "tr", voice.Override{Speed: 1.2},
			voice.Config{Engine: voice.EngineEMA, Voice: "ema", Speed: 1.2}},
		{"a voice selects its engine", "tr", voice.Override{Voice: "okur"},
			voice.Config{Engine: voice.EngineOkur, Voice: "okur", Speed: 1}},
		{"an engine alone uses its default voice", "tr", voice.Override{Engine: voice.EngineElevenLabs},
			voice.Config{Engine: voice.EngineElevenLabs, Voice: voice.ElevenLabsDefaultVoice, Speed: 1}},
		{"an account voice stays on elevenlabs", "en", voice.Override{Voice: "pNInz6obpgDQGcFmaJgB"},
			voice.Config{Engine: voice.EngineElevenLabs, Voice: "pNInz6obpgDQGcFmaJgB", Speed: 1.1}},
		{"a kokoro voice switches away from elevenlabs", "en", voice.Override{Voice: "bm_george"},
			voice.Config{Engine: voice.EngineKokoro, Voice: "bm_george", Speed: 1}},
		{"speed is held to the engine's range", "en", voice.Override{Speed: 2},
			voice.Config{Engine: voice.EngineElevenLabs, Voice: "21m00Tcm4TlvDq8ikWAM", Speed: 1.2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.o.Validate(tt.lang); err != nil {
				t.Fatal(err)
			}
			c := s.For(tt.lang, &tt.o)
			if c.Engine != tt.want.Engine || c.Voice != tt.want.Voice || c.Speed != tt.want.Speed {
				t.Fatalf("got %s/%s/%v, want %s/%s/%v", c.Engine, c.Voice, c.Speed, tt.want.Engine, tt.want.Voice, tt.want.Speed)
			}
			if c.Engine != s.Languages[tt.lang].Engine && c.SentencePause != s.Languages[tt.lang].SentencePause {
				t.Fatalf("switching engines lost the pauses: %+v", c)
			}
			if (c.Engine == voice.EngineElevenLabs) != (c.ElevenLabs != nil) {
				t.Fatalf("elevenlabs settings = %+v for %s", c.ElevenLabs, c.Engine)
			}
		})
	}

	bad := []struct {
		lang string
		c    voice.Config
	}{
		{"en", voice.Config{Engine: voice.EngineEMA, Voice: "ema", Speed: 1}},
		{"tr", voice.Config{Engine: "piper", Voice: "x", Speed: 1}},
		{"tr", voice.Config{Engine: voice.EngineEMA, Voice: "okur", Speed: 1}},
		{"tr", voice.Config{Engine: voice.EngineElevenLabs, Voice: "short", Speed: 1}},
		{"tr", voice.Config{Engine: voice.EngineElevenLabs, Voice: voice.ElevenLabsDefaultVoice, Speed: 1.5}},
		{"tr", voice.Config{Engine: voice.EngineElevenLabs, Voice: voice.ElevenLabsDefaultVoice, Speed: 1,
			ElevenLabs: &voice.ElevenLabs{Model: "eleven_v3", Stability: 1.5}}},
		{"tr", voice.Config{Engine: voice.EngineElevenLabs, Voice: voice.ElevenLabsDefaultVoice, Speed: 1,
			ElevenLabs: &voice.ElevenLabs{Model: "../../v1/user"}}},
	}
	for i, b := range bad {
		if err := b.c.Validate(b.lang); !apperror.Is(err, apperror.ValidationFailed) {
			t.Errorf("bad %d accepted: %v", i, err)
		}
	}
	if err := (voice.Override{Engine: voice.EngineKokoro}).Validate("tr"); err == nil {
		t.Fatal("an English engine was accepted for Turkish")
	}
}
