// Package voice models how narration sounds: per language the TTS engine,
// its voice, speed and rhythm, a pronunciation dictionary for names and
// acronyms, and per-pipeline overrides.
package voice

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"kurgu/internal/apperror"
)

// Engine IDs.
const (
	EngineOkur       = "okur"
	EngineEMA        = "ema"
	EngineKokoro     = "kokoro"
	EngineElevenLabs = "elevenlabs"
)

// Voice is one selectable voice.
type Voice struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Accent string `json:"accent"` // tr, en-us, en-gb
	Gender string `json:"gender"` // female, male
}

// Engine is a TTS engine for one language and what it can do. An engine
// that reads several languages has one entry per language.
type Engine struct {
	ID           string  `json:"id"`
	Language     string  `json:"language"`
	Name         string  `json:"name"`
	Voices       []Voice `json:"voices"` // empty when the voices come from the provider's account
	MinSpeed     float64 `json:"minSpeed"`
	MaxSpeed     float64 `json:"maxSpeed"`
	HasSeed      bool    `json:"hasSeed"`      // other takes of the same text
	HasQuality   bool    `json:"hasQuality"`   // kokoro: model precision
	Cloud        bool    `json:"cloud"`        // a paid API that needs a key; text leaves the server
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

// ElevenLabsDefaultVoice is a premade multilingual voice (George) every
// ElevenLabs account has.
const ElevenLabsDefaultVoice = "JBFqnCBsd6RMkjVDRZzb"

// Catalog lists the engines Kurgu ships, by language. The first engine of
// a language is its default.
var Catalog = []Engine{
	{ID: EngineOkur, Language: "tr", Name: "okur-tts", Voices: []Voice{{ID: "okur", Label: "Okur (Turkish)", Accent: "tr", Gender: "female"}},
		MinSpeed: 0.5, MaxSpeed: 2, HasSeed: true, DefaultVoice: "okur"},
	{ID: EngineEMA, Language: "tr", Name: "EMA Lightning", Voices: []Voice{{ID: "ema", Label: "EMA (Turkish)", Accent: "tr", Gender: "male"}},
		MinSpeed: 0.5, MaxSpeed: 2, HasSeed: true, DefaultVoice: "ema"},
	{ID: EngineElevenLabs, Language: "tr", Name: "ElevenLabs", MinSpeed: 0.7, MaxSpeed: 1.2, HasSeed: true, Cloud: true,
		DefaultVoice: ElevenLabsDefaultVoice},
	{ID: EngineKokoro, Language: "en", Name: "Kokoro-82M", Voices: kokoroVoices(), MinSpeed: 0.5, MaxSpeed: 2, HasQuality: true, DefaultVoice: "af_heart"},
	{ID: EngineElevenLabs, Language: "en", Name: "ElevenLabs", MinSpeed: 0.7, MaxSpeed: 1.2, HasSeed: true, Cloud: true,
		DefaultVoice: ElevenLabsDefaultVoice},
}

// EnginesFor lists the engines of a language, default first.
func EnginesFor(language string) []Engine {
	var out []Engine
	for _, e := range Catalog {
		if e.Language == language {
			out = append(out, e)
		}
	}
	return out
}

// EngineFor returns an engine of a language; id "" is the language's
// default engine.
func EngineFor(language, id string) (Engine, error) {
	for _, e := range EnginesFor(language) {
		if id == "" || e.ID == id {
			return e, nil
		}
	}
	if id == "" {
		return Engine{}, apperror.New(apperror.ValidationFailed, "no voice for language "+language)
	}
	return Engine{}, apperror.New(apperror.ValidationFailed, fmt.Sprintf("%s: unknown engine %s", language, id))
}

var providerVoiceID = regexp.MustCompile(`^[A-Za-z0-9]{8,64}$`)

// HasVoice reports whether voice can be used with e: one of its voices,
// or for an engine whose voices live in the provider's account, an ID of
// the provider's shape.
func (e Engine) HasVoice(voice string) bool {
	if len(e.Voices) == 0 {
		return providerVoiceID.MatchString(voice)
	}
	return slices.ContainsFunc(e.Voices, func(v Voice) bool { return v.ID == voice })
}

// engineOfVoice is the engine of a language that has voice, preferring
// engines that list their voices; "" when none does.
func engineOfVoice(language, voice string) string {
	for _, e := range EnginesFor(language) {
		if len(e.Voices) > 0 && e.HasVoice(voice) {
			return e.ID
		}
	}
	for _, e := range EnginesFor(language) {
		if e.HasVoice(voice) {
			return e.ID
		}
	}
	return ""
}

// ElevenLabs are the settings of the ElevenLabs engine.
type ElevenLabs struct {
	Model           string  `json:"model"`           // e.g. eleven_multilingual_v2
	Stability       float64 `json:"stability"`       // 0..1: higher is steadier, lower more expressive
	SimilarityBoost float64 `json:"similarityBoost"` // 0..1: how closely to keep to the original voice
	Style           float64 `json:"style"`           // 0..1: style exaggeration
	SpeakerBoost    bool    `json:"speakerBoost"`
}

// ElevenLabsDefaults are ElevenLabs' own defaults.
func ElevenLabsDefaults() ElevenLabs {
	return ElevenLabs{Model: "eleven_multilingual_v2", Stability: 0.5, SimilarityBoost: 0.75, SpeakerBoost: true}
}

var modelID = regexp.MustCompile(`^[a-z0-9_]{3,64}$`)

// Config is how one language is narrated.
type Config struct {
	Engine         string      `json:"engine"`
	Voice          string      `json:"voice"`
	Speed          float64     `json:"speed"`
	SentencePause  float64     `json:"sentencePause"`     // seconds
	ParagraphPause float64     `json:"paragraphPause"`    // seconds
	Seed           int         `json:"seed,omitempty"`    // okur, ema, elevenlabs
	Quality        string      `json:"quality,omitempty"` // kokoro: fp32 | fp16 | int8
	ElevenLabs     *ElevenLabs `json:"elevenlabs,omitempty"`
}

// Settings are a workspace's voice settings.
type Settings struct {
	Languages map[string]Config `json:"languages"`
	// Lexicon maps a written word to how it is spoken, per language.
	// Captions keep the written word.
	Lexicon map[string]map[string]string `json:"lexicon"`
}

// Override changes the engine, voice or speed for one pipeline or
// request. A voice without an engine selects the engine that has it.
type Override struct {
	Engine string  `json:"engine,omitempty"`
	Voice  string  `json:"voice,omitempty"`
	Speed  float64 `json:"speed,omitempty"`
}

// MaxLexicon bounds a language's pronunciation dictionary.
const MaxLexicon = 500

// Defaults are the settings before a workspace changes anything.
func Defaults() Settings {
	return Settings{
		Languages: map[string]Config{"tr": DefaultConfig("tr", ""), "en": DefaultConfig("en", "")},
		Lexicon:   map[string]map[string]string{},
	}
}

// DefaultConfig is how a language is narrated with an engine ("" for the
// language's default) before anything is changed.
func DefaultConfig(language, engine string) Config {
	e, err := EngineFor(language, engine)
	if err != nil {
		return Config{}
	}
	c := Config{Engine: e.ID, Voice: e.DefaultVoice, Speed: 1, SentencePause: 0.25, ParagraphPause: 0.5}
	switch e.ID {
	case EngineKokoro:
		c.Quality = "fp32"
	case EngineElevenLabs:
		el := ElevenLabsDefaults()
		c.ElevenLabs = &el
	}
	return c
}

// Normalize fills missing languages and fields with defaults. Settings
// saved before engines were selectable get their language's default
// engine.
func (s Settings) Normalize() Settings {
	out := Settings{Languages: map[string]Config{}, Lexicon: map[string]map[string]string{}}
	for lang, def := range Defaults().Languages {
		c, ok := s.Languages[lang]
		if !ok {
			c = def
		}
		out.Languages[lang] = c.Normalize(lang)
	}
	for lang, lex := range s.Lexicon {
		out.Lexicon[lang] = lex
	}
	return out
}

// Normalize fills c's missing fields with its engine's defaults and drops
// settings of other engines.
func (c Config) Normalize(lang string) Config {
	if c.Engine == "" {
		c.Engine = DefaultConfig(lang, "").Engine
	}
	def := DefaultConfig(lang, c.Engine)
	if def.Engine == "" {
		return c // unknown engine: Validate reports it
	}
	if c.Voice == "" {
		c.Voice = def.Voice
	}
	if c.Speed == 0 {
		c.Speed = def.Speed
	}
	if c.Engine == EngineKokoro && c.Quality == "" {
		c.Quality = def.Quality
	}
	if c.Engine != EngineKokoro {
		c.Quality = ""
	}
	if c.Engine == EngineElevenLabs {
		if c.ElevenLabs == nil {
			c.ElevenLabs = def.ElevenLabs
		} else if c.ElevenLabs.Model == "" {
			el := *c.ElevenLabs
			el.Model = def.ElevenLabs.Model
			c.ElevenLabs = &el
		}
	} else {
		c.ElevenLabs = nil
	}
	return c
}

// Validate checks every language's config against its engine.
func (s Settings) Validate() error {
	for lang, c := range s.Languages {
		if err := c.Validate(lang); err != nil {
			return err
		}
	}
	for lang, lex := range s.Lexicon {
		if _, err := EngineFor(lang, ""); err != nil {
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

// Validate checks c for a language. An empty engine is the language's
// default.
func (c Config) Validate(lang string) error {
	e, err := EngineFor(lang, c.Engine)
	if err != nil {
		return err
	}
	fail := func(m string) error { return apperror.New(apperror.ValidationFailed, lang+": "+m) }
	if !e.HasVoice(c.Voice) {
		return fail("unknown voice " + c.Voice + " for " + e.Name)
	}
	if c.Speed < e.MinSpeed || c.Speed > e.MaxSpeed || math.IsNaN(c.Speed) {
		return fail(fmt.Sprintf("%s speed must be between %.2g and %.2g", e.Name, e.MinSpeed, e.MaxSpeed))
	}
	for _, p := range []float64{c.SentencePause, c.ParagraphPause} {
		if p < 0 || p > 3 || math.IsNaN(p) {
			return fail("pauses must be between 0 and 3 seconds")
		}
	}
	if c.Seed < 0 || c.Seed > 1_000_000 {
		return fail("seed must be 0 to 1000000")
	}
	if c.Quality != "" && c.Quality != "fp32" && c.Quality != "fp16" && c.Quality != "int8" {
		return fail("quality must be fp32, fp16 or int8")
	}
	if el := c.ElevenLabs; el != nil {
		if el.Model != "" && !modelID.MatchString(el.Model) {
			return fail("unknown ElevenLabs model " + el.Model)
		}
		for _, v := range []float64{el.Stability, el.SimilarityBoost, el.Style} {
			if v < 0 || v > 1 || math.IsNaN(v) {
				return fail("ElevenLabs stability, similarity and style must be between 0 and 1")
			}
		}
	}
	return nil
}

// Validate checks an override for a language.
func (o Override) Validate(lang string) error {
	engine := o.Engine
	if engine == "" && o.Voice != "" {
		if engine = engineOfVoice(lang, o.Voice); engine == "" {
			return apperror.New(apperror.ValidationFailed, lang+": unknown voice "+o.Voice)
		}
	}
	return DefaultConfig(lang, "").apply(lang, &Override{Engine: engine, Voice: o.Voice, Speed: o.Speed}).Validate(lang)
}

// apply returns c changed by o. Switching engines starts from the new
// engine's defaults and keeps the pauses and the seed.
func (c Config) apply(lang string, o *Override) Config {
	if o == nil {
		return c
	}
	engine := o.Engine
	if engine == "" && o.Voice != "" && !mustEngine(lang, c.Engine).HasVoice(o.Voice) {
		engine = engineOfVoice(lang, o.Voice)
	}
	if engine != "" && engine != c.Engine {
		next := DefaultConfig(lang, engine)
		if next.Engine == "" {
			next = Config{Engine: engine, Speed: 1} // unknown engine: Validate reports it
		}
		next.SentencePause, next.ParagraphPause, next.Seed = c.SentencePause, c.ParagraphPause, c.Seed
		c = next
	}
	if o.Voice != "" {
		c.Voice = o.Voice
	}
	if o.Speed != 0 {
		c.Speed = o.Speed
		// An override is checked against the default engine; the
		// workspace's engine may allow a narrower range.
		if e := mustEngine(lang, c.Engine); e.ID != "" {
			c.Speed = min(max(c.Speed, e.MinSpeed), e.MaxSpeed)
		}
	}
	return c
}

func mustEngine(lang, id string) Engine {
	e, _ := EngineFor(lang, id)
	return e
}

// For returns the effective config of a language with an override.
func (s Settings) For(lang string, o *Override) Config {
	return s.Normalize().Languages[lang].apply(lang, o)
}
