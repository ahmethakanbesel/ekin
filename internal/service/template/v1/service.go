// Package template serves house styles: the built-ins shipped in
// builtin/*.md, the workspace's own, and drafts the agent writes from a
// channel the user names.
package template

import (
	"bytes"
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/ahmethakanbesel/ekin/internal/adapter/claude"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	templatedomain "github.com/ahmethakanbesel/ekin/internal/domain/template/v1"
	templaterepo "github.com/ahmethakanbesel/ekin/internal/repository/template/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// KindGenerate is the job that drafts a template from a channel.
const KindGenerate = "template.generate"

// GeneratePayload is the template.generate job payload.
type GeneratePayload struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	Channel     string    `json:"channel"`
	Notes       string    `json:"notes,omitempty"`
	Kinds       []string  `json:"kinds"`
}

//go:embed builtin/*.md
var builtinFS embed.FS

type frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Inspiration string   `yaml:"inspiration"`
	Kinds       []string `yaml:"kinds"`
	Palette     []string `yaml:"palette"`
	HeadingFont string   `yaml:"headingFont"`
	BodyFont    string   `yaml:"bodyFont"`
	MusicMood   string   `yaml:"musicMood"`
}

// loadBuiltins parses builtin/*.md; it panics on a malformed file, which
// tests catch.
func loadBuiltins() []templatedomain.Template {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		panic(err)
	}
	var out []templatedomain.Template
	for _, e := range entries {
		raw, err := builtinFS.ReadFile(path.Join("builtin", e.Name()))
		if err != nil {
			panic(err)
		}
		parts := bytes.SplitN(raw, []byte("---\n"), 3)
		if len(parts) != 3 {
			panic("template " + e.Name() + ": missing frontmatter")
		}
		var fm frontmatter
		if err := yaml.Unmarshal(parts[1], &fm); err != nil {
			panic("template " + e.Name() + ": " + err.Error())
		}
		kinds := make([]pipelinedomain.Kind, 0, len(fm.Kinds))
		for _, k := range fm.Kinds {
			kinds = append(kinds, pipelinedomain.Kind(k))
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		t, err := templatedomain.New(templatedomain.BuiltinID(slug), uuid.Nil, templatedomain.Spec{
			Name: fm.Name, Description: fm.Description, Inspiration: fm.Inspiration, Kinds: kinds,
			Guide: string(parts[2]), Palette: fm.Palette, HeadingFont: fm.HeadingFont, BodyFont: fm.BodyFont, MusicMood: fm.MusicMood,
		}, templatedomain.SourceBuiltin, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
		if err != nil {
			panic("template " + e.Name() + ": " + err.Error())
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b templatedomain.Template) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// Agent runs Claude Code.
type Agent interface {
	Run(ctx context.Context, req claude.Request) (claude.Result, error)
}

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// Service runs template use cases.
type Service struct {
	repo     templaterepo.Repository
	builtins []templatedomain.Template
	jobs     Enqueuer
	agent    Agent
	workDir  string
	now      func() time.Time
}

// NewService returns a Service.
func NewService(repo templaterepo.Repository, jobs Enqueuer, agent Agent, workDir string) *Service {
	return &Service{repo: repo, builtins: loadBuiltins(), jobs: jobs, agent: agent, workDir: workDir, now: time.Now}
}

// List returns the built-ins followed by the workspace's templates.
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID) ([]templatedomain.Template, error) {
	own, err := s.repo.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return append(slices.Clone(s.builtins), own...), nil
}

// Get returns a built-in or one of the workspace's templates.
func (s *Service) Get(ctx context.Context, workspaceID, templateID uuid.UUID) (templatedomain.Template, error) {
	for _, t := range s.builtins {
		if t.ID == templateID {
			return t, nil
		}
	}
	return s.repo.Get(ctx, workspaceID, templateID)
}

// Check verifies a pipeline may use templateID for kinds.
func (s *Service) Check(ctx context.Context, workspaceID, templateID uuid.UUID, kinds []pipelinedomain.Kind) error {
	t, err := s.Get(ctx, workspaceID, templateID)
	if apperror.Is(err, apperror.NotFound) {
		return apperror.New(apperror.ValidationFailed, "the chosen template does not exist")
	}
	if err != nil {
		return err
	}
	for _, k := range kinds {
		if t.Supports(k) {
			return nil
		}
	}
	return apperror.New(apperror.ValidationFailed, "template "+t.Name+" is not for this kind of output")
}

// Create adds a workspace template.
func (s *Service) Create(ctx context.Context, workspaceID uuid.UUID, spec templatedomain.Spec) (templatedomain.Template, error) {
	t, err := templatedomain.New(id.New(), workspaceID, spec, templatedomain.SourceCustom, s.now())
	if err != nil {
		return t, err
	}
	return t, s.repo.Create(ctx, t)
}

// Update changes a workspace template. version is the version the caller
// read (0: whatever is stored now); a newer stored version fails with
// StaleVersion instead of overwriting someone else's edit, and a template
// deleted meanwhile stays deleted.
func (s *Service) Update(ctx context.Context, workspaceID, templateID uuid.UUID, version int64, spec templatedomain.Spec) (templatedomain.Template, error) {
	t, err := s.Get(ctx, workspaceID, templateID)
	if err != nil {
		return t, err
	}
	if version != 0 && version != t.Version {
		return t, apperror.New(apperror.StaleVersion, "the template was changed by someone else; reload and retry")
	}
	if err := t.Update(spec, s.now()); err != nil {
		return t, err
	}
	return s.repo.Update(ctx, t)
}

// Duplicate copies any template into the workspace as an editable one.
func (s *Service) Duplicate(ctx context.Context, workspaceID, templateID uuid.UUID) (templatedomain.Template, error) {
	src, err := s.Get(ctx, workspaceID, templateID)
	if err != nil {
		return src, err
	}
	spec := src.Spec()
	spec.Name = truncate(spec.Name+" (copy)", templatedomain.MaxNameLength)
	return s.Create(ctx, workspaceID, spec)
}

// Delete removes a workspace template. Pipelines that used it keep
// working without a template.
func (s *Service) Delete(ctx context.Context, workspaceID, templateID uuid.UUID) error {
	for _, t := range s.builtins {
		if t.ID == templateID {
			return apperror.New(apperror.Forbidden, "built-in templates cannot be deleted")
		}
	}
	return s.repo.Delete(ctx, workspaceID, templateID)
}

// QueueGenerate asks the agent to draft a template from a channel.
func (s *Service) QueueGenerate(ctx context.Context, workspaceID uuid.UUID, channel, notes string, kinds []pipelinedomain.Kind) (*jobdomain.Job, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" || len(channel) > 300 {
		return nil, apperror.New(apperror.ValidationFailed, "name the channel (or paste its URL)")
	}
	if len(notes) > 2000 {
		return nil, apperror.New(apperror.ValidationFailed, "notes are too long")
	}
	if len(kinds) == 0 {
		kinds = []pipelinedomain.Kind{pipelinedomain.KindVideo}
	}
	ks := make([]string, 0, len(kinds))
	for _, k := range kinds {
		ks = append(ks, string(k))
	}
	payload, _ := json.Marshal(GeneratePayload{WorkspaceID: workspaceID, Channel: channel, Notes: strings.TrimSpace(notes), Kinds: ks})
	j, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{WorkspaceID: workspaceID, Queue: jobdomain.QueueAgent, Kind: KindGenerate, Payload: payload, MaxAttempts: 1})
	return j, err
}

var generateSchema = json.RawMessage(`{
  "type": "object",
  "required": ["name", "description", "guide", "palette", "headingFont", "bodyFont", "musicMood"],
  "properties": {
    "name": {"type": "string", "description": "a descriptive style name WITHOUT the channel's name, at most 60 characters"},
    "description": {"type": "string", "description": "one sentence"},
    "guide": {"type": "string", "description": "Markdown style guide with sections: Look, Typography, Motion, Structure and pacing, Narration voice, Captions (and Articles if relevant)"},
    "palette": {"type": "array", "items": {"type": "string", "description": "#RRGGBB"}, "maxItems": 8},
    "headingFont": {"type": "string", "enum": ["Inter", "Lora", "Nunito", "Oswald", "Caveat", "JetBrains Mono", "Fraunces", "Instrument Serif"]},
    "bodyFont": {"type": "string", "enum": ["Inter", "Lora", "Nunito", "Oswald", "Caveat", "JetBrains Mono", "Fraunces", "Instrument Serif"]},
    "musicMood": {"type": "string"}
  }
}`)

// Generate runs a template.generate job.
func (s *Service) Generate(ctx context.Context, p GeneratePayload) error {
	var example string
	if len(s.builtins) > 0 {
		example = s.builtins[0].Guide
	}
	prompt := fmt.Sprintf(`Write a reusable visual and editorial style template inspired by the YouTube channel %q.
%s
Research the channel with web search: how its videos look (palette, shapes, illustration vs. footage, textures), typography, motion and transitions, how episodes are structured and paced, the narrator's voice, and how it uses music. Rely on several sources; do not guess.

The template drives an AI that builds videos as animated HTML/CSS/SVG compositions (no footage of people speaking, no 3D) with fonts limited to Inter, Lora, Nunito, Oswald, Caveat, JetBrains Mono, Fraunces and Instrument Serif, and karaoke captions. Translate the channel's style into what can be built that way.

Describe the style, never copy it: the guide must tell the AI not to use the channel's name, logo, mascots, characters, catchphrases, music or title cards, and the template name must not contain the channel's name.

Template for kinds: %s.

Here is an example of the expected depth and format of "guide":

%s`, p.Channel, notesLine(p.Notes), strings.Join(p.Kinds, ", "), example)
	res, err := s.agent.Run(ctx, claude.Request{
		Dir: s.workDir, Prompt: prompt, JSONSchema: generateSchema,
		Tools: []string{"WebSearch", "WebFetch"}, Allowed: []string{"WebSearch", "WebFetch"}, Timeout: 15 * time.Minute,
	})
	if err != nil {
		return jobdomain.Permanent(fmt.Errorf("template draft: %w", err))
	}
	var out struct {
		Name, Description, Guide, HeadingFont, BodyFont, MusicMood string
		Palette                                                    []string
	}
	if err := json.Unmarshal(res.Structured, &out); err != nil {
		return jobdomain.Permanent(err)
	}
	kinds := make([]pipelinedomain.Kind, 0, len(p.Kinds))
	for _, k := range p.Kinds {
		kinds = append(kinds, pipelinedomain.Kind(k))
	}
	t, err := templatedomain.New(id.New(), p.WorkspaceID, templatedomain.Spec{
		Name: truncate(out.Name, templatedomain.MaxNameLength), Description: out.Description,
		Inspiration: truncate(p.Channel+"-style (drafted by the agent)", 300), Kinds: kinds, Guide: out.Guide,
		Palette: validColours(out.Palette), HeadingFont: out.HeadingFont, BodyFont: out.BodyFont, MusicMood: out.MusicMood,
	}, templatedomain.SourceGenerated, s.now())
	if err != nil {
		return jobdomain.Permanent(err)
	}
	return s.repo.Create(ctx, t)
}

func notesLine(notes string) string {
	if notes == "" {
		return ""
	}
	return "\nThe person asking adds: " + notes + "\n"
}

func validColours(cs []string) []string {
	var out []string
	for _, c := range cs {
		if len(c) == 7 && c[0] == '#' && len(out) < templatedomain.MaxPalette {
			out = append(out, c)
		}
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// Style returns the style a story keeps of the template.
func (s *Service) Style(ctx context.Context, workspaceID, templateID uuid.UUID) (*storydomain.TemplateStyle, error) {
	t, err := s.Get(ctx, workspaceID, templateID)
	if err != nil {
		return nil, err
	}
	return t.Style(), nil
}
