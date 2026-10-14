// Package voice serves a workspace's narration settings, resolves the
// voice a deliverable is narrated with, and renders voice previews.
package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/elevenlabs"
	"kurgu/internal/adapter/speech"
	"kurgu/internal/apperror"
	"kurgu/internal/blob"
	jobdomain "kurgu/internal/domain/job/v1"
	voicedomain "kurgu/internal/domain/voice/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	"kurgu/pkg/id"
)

// KindPreview renders a short voice sample.
const KindPreview = "voice.preview"

// PreviewPayload is the voice.preview job payload.
type PreviewPayload struct {
	WorkspaceID uuid.UUID          `json:"workspaceId"`
	PreviewID   uuid.UUID          `json:"previewId"`
	Language    string             `json:"language"`
	Config      voicedomain.Config `json:"config"`
	Text        string             `json:"text"`
}

// Speaker synthesises speech (internal/adapter/speech).
type Speaker interface {
	Say(ctx context.Context, lang, textFile, outDir string, o speech.Options) (speech.Timings, error)
}

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// Service runs voice use cases.
type Service struct {
	settings settingsrepo.Repository
	jobs     Enqueuer
	speaker  Speaker
	blobs    blob.Store
	now      func() time.Time
	// elevenLabs returns a client with the current API key.
	elevenLabs func(context.Context) *elevenlabs.Client
}

// NewService returns a Service. speaker may be nil in processes that do
// not render previews.
func NewService(settings settingsrepo.Repository, jobs Enqueuer, speaker Speaker, blobs blob.Store) *Service {
	return &Service{settings: settings, jobs: jobs, speaker: speaker, blobs: blobs, now: time.Now}
}

// WithElevenLabs lets the service read the ElevenLabs account.
func (s *Service) WithElevenLabs(client func(context.Context) *elevenlabs.Client) *Service {
	s.elevenLabs = client
	return s
}

func key(ws uuid.UUID) string { return "voice:" + ws.String() }

// Get returns the workspace's settings, defaults filled in.
func (s *Service) Get(ctx context.Context, workspaceID uuid.UUID) (voicedomain.Settings, error) {
	var v voicedomain.Settings
	err := s.settings.Get(ctx, key(workspaceID), &v)
	if apperror.Is(err, apperror.NotFound) {
		return voicedomain.Defaults(), nil
	}
	if err != nil {
		return v, err
	}
	return v.Normalize(), nil
}

// Save validates and stores the workspace's settings.
func (s *Service) Save(ctx context.Context, workspaceID, by uuid.UUID, v voicedomain.Settings) (voicedomain.Settings, error) {
	v = v.Normalize()
	if err := v.Validate(); err != nil {
		return v, err
	}
	for _, c := range v.Languages {
		if err := s.requireKey(ctx, c.Engine); err != nil {
			return v, err
		}
	}
	return v, s.settings.Put(ctx, key(workspaceID), v, by, s.now())
}

// CheckOverride verifies a pipeline's or request's voice override.
func (s *Service) CheckOverride(ctx context.Context, language string, o voicedomain.Override) error {
	if err := o.Validate(language); err != nil {
		return err
	}
	return s.requireKey(ctx, o.Engine)
}

// requireKey refuses the ElevenLabs engine while no API key is set:
// every narration with it would fail.
func (s *Service) requireKey(ctx context.Context, engine string) error {
	if engine != voicedomain.EngineElevenLabs {
		return nil
	}
	if s.elevenLabs == nil || s.elevenLabs(ctx).APIKey == "" {
		return apperror.New(apperror.ValidationFailed, "ElevenLabs needs an API key: add it under System → Credentials")
	}
	return nil
}

// Options returns the speech options a deliverable in language is
// narrated with: workspace settings, then the override, plus the
// language's pronunciation dictionary.
func (s *Service) Options(ctx context.Context, workspaceID uuid.UUID, language string, o *voicedomain.Override) (speech.Options, error) {
	v, err := s.Get(ctx, workspaceID)
	if err != nil {
		return speech.Options{}, err
	}
	return toOptions(v.For(language, o), v.Lexicon[language]), nil
}

func toOptions(c voicedomain.Config, lexicon map[string]string) speech.Options {
	o := speech.Options{
		Engine: c.Engine, Voice: c.Voice, Speed: c.Speed, Seed: c.Seed, SentencePause: c.SentencePause,
		ParagraphPause: c.ParagraphPause, Quality: c.Quality, Lexicon: lexicon,
	}
	if el := c.ElevenLabs; el != nil {
		o.ElevenLabs = &speech.ElevenLabsOptions{Model: el.Model, Stability: el.Stability, SimilarityBoost: el.SimilarityBoost,
			Style: el.Style, SpeakerBoost: el.SpeakerBoost}
	}
	return o
}

// ElevenLabsAccount is what the voice settings show of the ElevenLabs
// account.
type ElevenLabsAccount struct {
	Configured   bool
	Error        string // set when the account could not be read
	Subscription *elevenlabs.Subscription
	Voices       []elevenlabs.Voice
	Models       []elevenlabs.Model
}

// MaxElevenLabsVoices bounds the voices listed.
const MaxElevenLabsVoices = 300

// ElevenLabs reads the account's voices, models and usage. A missing key
// is not an error: Configured is false.
func (s *Service) ElevenLabs(ctx context.Context) (ElevenLabsAccount, error) {
	var c *elevenlabs.Client
	if s.elevenLabs != nil {
		c = s.elevenLabs(ctx)
	}
	if c == nil || c.APIKey == "" {
		return ElevenLabsAccount{}, nil
	}
	out := ElevenLabsAccount{Configured: true}
	var err error
	if out.Voices, err = c.Voices(ctx, MaxElevenLabsVoices); err != nil {
		out.Error = err.Error()
		return out, nil
	}
	if out.Models, err = c.Models(ctx); err != nil {
		out.Error = err.Error()
		return out, nil
	}
	// A key limited to text to speech cannot read usage; that is fine.
	if sub, err := c.Subscription(ctx); err == nil {
		out.Subscription = &sub
	}
	return out, nil
}

// MaxPreviewRunes bounds a preview's text.
const MaxPreviewRunes = 400

// QueuePreview renders text with config on a worker; fetch it with
// PreviewAudio(previewID) once the job finished.
func (s *Service) QueuePreview(ctx context.Context, workspaceID uuid.UUID, language string, config voicedomain.Config, text string) (uuid.UUID, *jobdomain.Job, error) {
	config = config.Normalize(language)
	if err := config.Validate(language); err != nil {
		return uuid.Nil, nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = map[string]string{
			"tr": "Merhaba. Bu ses, acme için hazırlanan videolarda kullanılacak. Kaygı ile endişe arasındaki farkı birlikte keşfedelim.",
			"en": "Hello. This is the voice your videos will use. Let's explore the difference between worry and anxiety together.",
		}[language]
	}
	if err := s.requireKey(ctx, config.Engine); err != nil {
		return uuid.Nil, nil, err
	}
	if len([]rune(text)) > MaxPreviewRunes {
		return uuid.Nil, nil, apperror.New(apperror.ValidationFailed, fmt.Sprintf("a preview is at most %d characters", MaxPreviewRunes))
	}
	pid := id.New()
	payload, _ := json.Marshal(PreviewPayload{WorkspaceID: workspaceID, PreviewID: pid, Language: language, Config: config, Text: text})
	j, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{WorkspaceID: workspaceID, Queue: jobdomain.QueueTTS, Kind: KindPreview, Payload: payload, MaxAttempts: 1})
	return pid, j, err
}

func previewKey(ws, pid uuid.UUID) string {
	return "ws/" + ws.String() + "/voice-previews/" + pid.String() + ".wav"
}

// Preview runs a voice.preview job.
func (s *Service) Preview(ctx context.Context, p PreviewPayload) error {
	if s.speaker == nil {
		return jobdomain.Permanent(fmt.Errorf("speech is not available on this worker"))
	}
	v, err := s.Get(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "kurgu-voice-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	textFile := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(textFile, []byte(p.Text), 0o600); err != nil {
		return err
	}
	if _, err := s.speaker.Say(ctx, p.Language, textFile, dir, toOptions(p.Config, v.Lexicon[p.Language])); err != nil {
		return jobdomain.Permanent(err)
	}
	f, err := os.Open(filepath.Join(dir, "speech.wav")) //nolint:gosec // our temp dir
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	st, _ := f.Stat()
	return s.blobs.Put(ctx, previewKey(p.WorkspaceID, p.PreviewID), f, st.Size())
}

// PreviewAudio opens a finished preview; NotFound until it is ready.
func (s *Service) PreviewAudio(ctx context.Context, workspaceID, previewID uuid.UUID) (io.ReadCloser, int64, error) {
	rc, info, err := s.blobs.Open(ctx, previewKey(workspaceID, previewID))
	if err != nil {
		return nil, 0, apperror.New(apperror.NotFound, "the preview is not ready yet")
	}
	return rc, info.Size, nil
}
