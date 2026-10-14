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
	if e, _ := voice.EngineFor("en"); len(e.Voices) != 28 {
		t.Fatalf("kokoro voices = %d", len(e.Voices))
	}
}
