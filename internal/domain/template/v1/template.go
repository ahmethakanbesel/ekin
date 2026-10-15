// Package template models reusable house styles: how a video looks and
// moves (palette, fonts, motion, structure, narration voice) or how an
// article reads. Built-in templates ship with Kurgu; a workspace adds its
// own, copies a built-in, or has the agent draft one from a channel.
package template

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	"kurgu/pkg/timeutil"
)

// Source says where a template came from.
type Source string

const (
	SourceBuiltin   Source = "builtin"
	SourceCustom    Source = "custom"
	SourceGenerated Source = "generated"
)

// Fonts are the families the composition kit bundles.
var Fonts = []string{"Inter", "Lora", "Nunito", "Oswald", "Caveat", "JetBrains Mono", "Fraunces", "Instrument Serif"}

// Limits.
const (
	MaxNameLength  = 80
	MaxGuideLength = 20000
	MaxPalette     = 8
)

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Template is a house style.
type Template struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID // Nil for built-ins
	Name        string
	Description string
	// Inspiration names what the style draws on (e.g. "Kurzgesagt-style
	// flat vector explainers"); it is guidance for people, never put on
	// screen.
	Inspiration string
	Kinds       []pipelinedomain.Kind
	Guide       string // Markdown: look, typography, motion, structure, voice, avoid
	Palette     []string
	HeadingFont string
	BodyFont    string
	MusicMood   string
	Source      Source
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Spec is the editable part of a template.
type Spec struct {
	Name, Description, Inspiration string
	Kinds                          []pipelinedomain.Kind
	Guide                          string
	Palette                        []string
	HeadingFont, BodyFont          string
	MusicMood                      string
}

// New validates spec and returns a workspace template.
func New(id, workspaceID uuid.UUID, spec Spec, source Source, now time.Time) (Template, error) {
	if id == uuid.Nil {
		return Template{}, apperror.New(apperror.ValidationFailed, "template id is required")
	}
	now = timeutil.Normalize(now)
	t := Template{ID: id, WorkspaceID: workspaceID, Source: source, CreatedAt: now}
	return t, t.apply(spec, now)
}

// Update replaces the editable fields. Built-ins are read-only.
func (t *Template) Update(spec Spec, now time.Time) error {
	if t.Source == SourceBuiltin {
		return apperror.New(apperror.Forbidden, "built-in templates are read-only; duplicate it to change it")
	}
	return t.apply(spec, timeutil.Normalize(now))
}

func (t *Template) apply(s Spec, now time.Time) error {
	fail := func(m string) error { return apperror.New(apperror.ValidationFailed, m) }
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || utf8.RuneCountInString(s.Name) > MaxNameLength {
		return fail("name must be 1 to 80 characters")
	}
	if utf8.RuneCountInString(s.Description) > 500 || utf8.RuneCountInString(s.Inspiration) > 300 || utf8.RuneCountInString(s.MusicMood) > 300 {
		return fail("description, inspiration or music mood is too long")
	}
	s.Guide = strings.TrimSpace(s.Guide)
	if s.Guide == "" || utf8.RuneCountInString(s.Guide) > MaxGuideLength {
		return fail("the style guide must be 1 to 20000 characters")
	}
	if len(s.Kinds) == 0 {
		return fail("pick video, article or both")
	}
	for _, k := range s.Kinds {
		if _, err := pipelinedomain.ParseKind(string(k)); err != nil {
			return err
		}
	}
	if len(s.Palette) > MaxPalette {
		return fail("a palette has at most 8 colours")
	}
	for i, c := range s.Palette {
		if !hexColor.MatchString(c) {
			return fail("colours must be #RRGGBB")
		}
		s.Palette[i] = strings.ToUpper(c)
	}
	for _, f := range []*string{&s.HeadingFont, &s.BodyFont} {
		if *f == "" {
			*f = "Inter"
		}
		if !slices.Contains(Fonts, *f) {
			return fail("fonts must be one of " + strings.Join(Fonts, ", "))
		}
	}
	t.Name, t.Description, t.Inspiration = s.Name, strings.TrimSpace(s.Description), strings.TrimSpace(s.Inspiration)
	t.Kinds, t.Guide, t.Palette = slices.Clone(s.Kinds), s.Guide, slices.Clone(s.Palette)
	t.HeadingFont, t.BodyFont, t.MusicMood = s.HeadingFont, s.BodyFont, strings.TrimSpace(s.MusicMood)
	t.UpdatedAt = now
	return nil
}

// Spec returns the editable fields, e.g. to duplicate.
func (t Template) Spec() Spec {
	return Spec{Name: t.Name, Description: t.Description, Inspiration: t.Inspiration, Kinds: slices.Clone(t.Kinds),
		Guide: t.Guide, Palette: slices.Clone(t.Palette), HeadingFont: t.HeadingFont, BodyFont: t.BodyFont, MusicMood: t.MusicMood}
}

// Supports reports whether the template applies to kind.
func (t Template) Supports(k pipelinedomain.Kind) bool { return slices.Contains(t.Kinds, k) }

// Style is what a story keeps of its template, so editing the template
// later does not change stories already in production.
func (t Template) Style() *storydomain.TemplateStyle {
	return &storydomain.TemplateStyle{
		ID: t.ID, Name: t.Name, Guide: t.Guide, Palette: slices.Clone(t.Palette),
		HeadingFont: t.HeadingFont, BodyFont: t.BodyFont, MusicMood: t.MusicMood, Kinds: kindsToStrings(t.Kinds),
	}
}

func kindsToStrings(ks []pipelinedomain.Kind) []string {
	out := make([]string, 0, len(ks))
	for _, k := range ks {
		out = append(out, string(k))
	}
	return out
}

// BuiltinID derives a stable id for a built-in template from its slug.
func BuiltinID(slug string) uuid.UUID {
	return uuid.NewSHA1(uuid.MustParse("6f1c2e0a-3b7d-4c5e-9a1f-0d2b4c6e8a10"), []byte("kurgu-template:"+slug))
}
