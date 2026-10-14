// Package voice models how narration sounds: per language the engine's
// voice, speed and rhythm, a pronunciation dictionary for names and
// acronyms, and per-pipeline overrides.
package voice

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"kurgu/internal/apperror"
)

// Voice is one selectable voice.
type Voice struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Accent string `json:"accent"` // tr, en-us, en-gb
	Gender string `json:"gender"` // female, male
}

// Engine describes the TTS engine for a language and what it can do.
type Engine struct {
	Language     string  `json:"language"`
	Name         string  `json:"name"`
	Voices       []Voice `json:"voices"`
	MinSpeed     float64 `json:"minSpeed"`
	MaxSpeed     float64 `json:"maxSpeed"`
	HasSeed      bool    `json:"hasSeed"`      // okur: other takes of the same text
	HasQuality   bool    `json:"hasQuality"`   // kokoro: model precision
	DefaultVoice string  `json:"defaultVoice"` // used when none is chosen
}

func kokoroVoices() []Voice {
	names := map[string][]string{
		"af": {"alloy", "aoede", "bella", "heart", "jessica", "kore", "nicole", "nova", "river", "sarah", "sky"},
		"am": {"adam", "echo", "eric", "fenrir", "liam", "michael", "onyx", "puck", "santa"},
		"bf": {"alice", "emma", "isabella", "lily"},
		"bm": {"daniel", "fable", "george", "lewis"},
	}
	var out []Voice
	for _, prefix := range []string{"af", "am", "bf", "bm"} {
		accent, accentLabel := "en-us", "American"
		if prefix[0] == 'b' {
			accent, accentLabel = "en-gb", "British"
		}
		gender := "female"
		if prefix[1] == 'm' {
			gender = "male"
		}
		for _, n := range names[prefix] {
			out = append(out, Voice{ID: prefix + "_" + n, Label: strings.ToUpper(n[:1]) + n[1:] + " (" + accentLabel + " " + gender + ")",
				Accent: accent, Gender: gender})
		}
	}
	return out
}

// Catalog lists the engines Kurgu ships, by language.
var Catalog = []Engine{
	{Language: "tr", Name: "okur-tts", Voices: []Voice{{ID: "okur", Label: "Okur (Turkish)", Accent: "tr", Gender: "female"}},
		MinSpeed: 0.5, MaxSpeed: 2, HasSeed: true, DefaultVoice: "okur"},
	{Language: "en", Name: "Kokoro-82M", Voices: kokoroVoices(), MinSpeed: 0.5, MaxSpeed: 2, HasQuality: true, DefaultVoice: "af_heart"},
}

// EngineFor returns the engine of a language.
func EngineFor(language string) (Engine, error) {
	for _, e := range Catalog {
		if e.Language == language {
			return e, nil
		}
	}
	return Engine{}, apperror.New(apperror.ValidationFailed, "no voice for language "+language)
}

// Config is how one language is narrated.
type Config struct {
	Voice          string  `json:"voice"`
	Speed          float64 `json:"speed"`
	SentencePause  float64 `json:"sentencePause"`     // seconds
	ParagraphPause float64 `json:"paragraphPause"`    // seconds
	Seed           int     `json:"seed,omitempty"`    // okur
	Quality        string  `json:"quality,omitempty"` // kokoro: fp32 | fp16 | int8
}

// Settings are a workspace's voice settings.
type Settings struct {
	Languages map[string]Config `json:"languages"`
	// Lexicon maps a written word to how it is spoken, per language.
	// Captions keep the written word.
	Lexicon map[string]map[string]string `json:"lexicon"`
}

// Override changes the voice or speed for one pipeline or request.
type Override struct {
	Voice string  `json:"voice,omitempty"`
	Speed float64 `json:"speed,omitempty"`
}

// MaxLexicon bounds a language's pronunciation dictionary.
const MaxLexicon = 500

// Defaults are the settings before a workspace changes anything.
func Defaults() Settings {
	return Settings{
		Languages: map[string]Config{
			"tr": {Voice: "okur", Speed: 1, SentencePause: 0.25, ParagraphPause: 0.5},
			"en": {Voice: "af_heart", Speed: 1, SentencePause: 0.25, ParagraphPause: 0.5, Quality: "fp32"},
		},
		Lexicon: map[string]map[string]string{},
	}
}

// Normalize fills missing languages and fields with defaults.
func (s Settings) Normalize() Settings {
	d := Defaults()
	out := Settings{Languages: map[string]Config{}, Lexicon: map[string]map[string]string{}}
	for lang, def := range d.Languages {
		c, ok := s.Languages[lang]
		if !ok {
			c = def
		}
		if c.Voice == "" {
			c.Voice = def.Voice
		}
		if c.Speed == 0 {
			c.Speed = def.Speed
		}
		if lang == "en" && c.Quality == "" {
			c.Quality = "fp32"
		}
		out.Languages[lang] = c
	}
	for lang, lex := range s.Lexicon {
		out.Lexicon[lang] = lex
	}
	return out
}

// Validate checks every language's config against its engine.
func (s Settings) Validate() error {
	for lang, c := range s.Languages {
		if err := c.Validate(lang); err != nil {
			return err
		}
	}
	for lang, lex := range s.Lexicon {
		if _, err := EngineFor(lang); err != nil {
			return err
		}
		if len(lex) > MaxLexicon {
			return apperror.New(apperror.ValidationFailed, fmt.Sprintf("at most %d pronunciations per language", MaxLexicon))
		}
		for w, sp := range lex {
			if strings.TrimSpace(w) == "" || strings.ContainsAny(w, " \t\n") {
				return apperror.New(apperror.ValidationFailed, "a pronunciation entry must be a single written word: "+w)
			}
			if strings.TrimSpace(sp) == "" || utf8.RuneCountInString(sp) > 100 || utf8.RuneCountInString(w) > 60 {
				return apperror.New(apperror.ValidationFailed, "pronunciation for "+w+" must be 1 to 100 characters")
			}
		}
	}
	return nil
}

// Validate checks c for a language.
func (c Config) Validate(lang string) error {
	e, err := EngineFor(lang)
	if err != nil {
		return err
	}
	fail := func(m string) error { return apperror.New(apperror.ValidationFailed, lang+": "+m) }
	if !slices.ContainsFunc(e.Voices, func(v Voice) bool { return v.ID == c.Voice }) {
		return fail("unknown voice " + c.Voice)
	}
	if c.Speed < e.MinSpeed || c.Speed > e.MaxSpeed || math.IsNaN(c.Speed) {
		return fail(fmt.Sprintf("speed must be between %.2g and %.2g", e.MinSpeed, e.MaxSpeed))
	}
	for _, p := range []float64{c.SentencePause, c.ParagraphPause} {
		if p < 0 || p > 3 {
			return fail("pauses must be between 0 and 3 seconds")
		}
	}
	if c.Seed < 0 || c.Seed > 1_000_000 {
		return fail("seed must be 0 to 1000000")
	}
	if c.Quality != "" && c.Quality != "fp32" && c.Quality != "fp16" && c.Quality != "int8" {
		return fail("quality must be fp32, fp16 or int8")
	}
	return nil
}

// Validate checks an override for a language.
func (o Override) Validate(lang string) error {
	c := Defaults().Languages[lang]
	if o.Voice != "" {
		c.Voice = o.Voice
	}
	if o.Speed != 0 {
		c.Speed = o.Speed
	}
	return c.Validate(lang)
}

// For returns the effective config of a language with an override.
func (s Settings) For(lang string, o *Override) Config {
	c := s.Normalize().Languages[lang]
	if o != nil {
		if o.Voice != "" {
			c.Voice = o.Voice
		}
		if o.Speed != 0 {
			c.Speed = o.Speed
		}
	}
	return c
}
