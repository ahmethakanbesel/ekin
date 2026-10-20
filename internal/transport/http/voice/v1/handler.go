// Package handler implements the voice operations of
// gen.StrictServerInterface.
package handler

import (
	"bytes"
	"context"
	"io"

	voicedomain "github.com/ahmethakanbesel/ekin/internal/domain/voice/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	voicesvc "github.com/ahmethakanbesel/ekin/internal/service/voice/v1"
	jobhandler "github.com/ahmethakanbesel/ekin/internal/transport/http/job/v1"
	"github.com/ahmethakanbesel/ekin/internal/transport/http/principal"
)

// Handler serves narration settings and previews.
type Handler struct{ svc *voicesvc.Service }

// New returns a Handler.
func New(svc *voicesvc.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListVoices(_ context.Context, _ gen.ListVoicesRequestObject) (gen.ListVoicesResponseObject, error) {
	out := gen.ListVoices200JSONResponse{Engines: make([]gen.VoiceEngine, 0, len(voicedomain.Catalog))}
	for _, e := range voicedomain.Catalog {
		voices := make([]gen.Voice, 0, len(e.Voices))
		for _, v := range e.Voices {
			voices = append(voices, gen.Voice{Id: v.ID, Label: v.Label, Accent: v.Accent, Gender: v.Gender})
		}
		out.Engines = append(out.Engines, gen.VoiceEngine{
			Id: gen.VoiceEngineId(e.ID), Language: gen.Language(e.Language), Name: e.Name, Voices: voices,
			MinSpeed: e.MinSpeed, MaxSpeed: e.MaxSpeed, HasSeed: e.HasSeed, HasQuality: e.HasQuality, Cloud: e.Cloud,
			DefaultVoice: e.DefaultVoice,
		})
	}
	return out, nil
}

func (h *Handler) GetVoiceSettings(ctx context.Context, req gen.GetVoiceSettingsRequestObject) (gen.GetVoiceSettingsResponseObject, error) {
	v, err := h.svc.Get(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.GetVoiceSettings200JSONResponse(settingsToWire(v)), nil
}

func (h *Handler) SaveVoiceSettings(ctx context.Context, req gen.SaveVoiceSettingsRequestObject) (gen.SaveVoiceSettingsResponseObject, error) {
	in := voicedomain.Settings{Languages: map[string]voicedomain.Config{}, Lexicon: req.Body.Lexicon}
	for lang, c := range req.Body.Languages {
		in.Languages[lang] = configOf(c)
	}
	pr, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.svc.Save(ctx, req.WorkspaceId, pr.User.ID(), in)
	if err != nil {
		return nil, err
	}
	return gen.SaveVoiceSettings200JSONResponse(settingsToWire(v)), nil
}

func (h *Handler) GetElevenLabsAccount(ctx context.Context, _ gen.GetElevenLabsAccountRequestObject) (gen.GetElevenLabsAccountResponseObject, error) {
	a, err := h.svc.ElevenLabs(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.GetElevenLabsAccount200JSONResponse{Configured: a.Configured, Voices: []gen.ElevenLabsVoice{}, Models: []gen.ElevenLabsModel{}}
	if a.Error != "" {
		out.Error = &a.Error
	}
	if sub := a.Subscription; sub != nil {
		out.Tier, out.CharacterCount, out.CharacterLimit = &sub.Tier, &sub.CharacterCount, &sub.CharacterLimit
	}
	for _, v := range a.Voices {
		w := gen.ElevenLabsVoice{Id: v.ID, Name: v.Name, Category: v.Category, Labels: v.Labels, Languages: v.Languages}
		if w.Labels == nil {
			w.Labels = map[string]string{}
		}
		if v.PreviewURL != "" {
			w.PreviewUrl = &v.PreviewURL
		}
		out.Voices = append(out.Voices, w)
	}
	for _, m := range a.Models {
		out.Models = append(out.Models, gen.ElevenLabsModel{Id: m.ID, Name: m.Name, Description: m.Description, Languages: m.Languages,
			MaxCharacters: m.MaxCharacters, CostMultiplier: m.CostMultiplier, CanStyle: m.CanStyle, CanSpeakerBoost: m.CanSpeakerBoost})
	}
	return out, nil
}

func (h *Handler) PreviewVoice(ctx context.Context, req gen.PreviewVoiceRequestObject) (gen.PreviewVoiceResponseObject, error) {
	var text string
	if req.Body.Text != nil {
		text = *req.Body.Text
	}
	pid, j, err := h.svc.QueuePreview(ctx, req.WorkspaceId, string(req.Body.Language), configOf(req.Body.Config), text)
	if err != nil {
		return nil, err
	}
	return gen.PreviewVoice202JSONResponse{PreviewId: pid, Job: jobhandler.ToWire(j)}, nil
}

func (h *Handler) GetVoicePreview(ctx context.Context, req gen.GetVoicePreviewRequestObject) (gen.GetVoicePreviewResponseObject, error) {
	rc, _, err := h.svc.PreviewAudio(ctx, req.WorkspaceId, req.PreviewId)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, 16<<20))
	if err != nil {
		return nil, err
	}
	return gen.GetVoicePreview200AudiowavResponse{Body: bytes.NewReader(b), ContentLength: int64(len(b))}, nil
}

// OverrideOf converts a wire override; nil when it changes nothing.
func OverrideOf(in *gen.VoiceOverride) *voicedomain.Override {
	if in == nil {
		return nil
	}
	var o voicedomain.Override
	if in.Engine != nil {
		o.Engine = string(*in.Engine)
	}
	if in.Voice != nil {
		o.Voice = *in.Voice
	}
	if in.Speed != nil {
		o.Speed = *in.Speed
	}
	if o == (voicedomain.Override{}) {
		return nil
	}
	return &o
}

// OverrideToWire converts an override; nil stays nil.
func OverrideToWire(o *voicedomain.Override) *gen.VoiceOverride {
	if o == nil {
		return nil
	}
	out := &gen.VoiceOverride{}
	if o.Engine != "" {
		e := gen.VoiceOverrideEngine(o.Engine)
		out.Engine = &e
	}
	if o.Voice != "" {
		out.Voice = &o.Voice
	}
	if o.Speed != 0 {
		out.Speed = &o.Speed
	}
	return out
}

func configOf(c gen.VoiceConfig) voicedomain.Config {
	out := voicedomain.Config{Voice: c.Voice, Speed: c.Speed, SentencePause: c.SentencePause, ParagraphPause: c.ParagraphPause}
	if c.Seed != nil {
		out.Seed = *c.Seed
	}
	if c.Quality != nil {
		out.Quality = string(*c.Quality)
	}
	if c.Engine != nil {
		out.Engine = string(*c.Engine)
	}
	if el := c.Elevenlabs; el != nil {
		out.ElevenLabs = &voicedomain.ElevenLabs{Model: el.Model, Stability: el.Stability, SimilarityBoost: el.SimilarityBoost,
			Style: el.Style, SpeakerBoost: el.SpeakerBoost}
	}
	return out
}

func settingsToWire(v voicedomain.Settings) gen.VoiceSettings {
	out := gen.VoiceSettings{Languages: map[string]gen.VoiceConfig{}, Lexicon: map[string]map[string]string{}}
	for lang, c := range v.Languages {
		engine := gen.VoiceConfigEngine(c.Engine)
		w := gen.VoiceConfig{Engine: &engine, Voice: c.Voice, Speed: c.Speed, SentencePause: c.SentencePause, ParagraphPause: c.ParagraphPause}
		if el := c.ElevenLabs; el != nil {
			w.Elevenlabs = &gen.ElevenLabsSettings{Model: el.Model, Stability: el.Stability, SimilarityBoost: el.SimilarityBoost,
				Style: el.Style, SpeakerBoost: el.SpeakerBoost}
		}
		if c.Seed != 0 {
			seed := c.Seed
			w.Seed = &seed
		}
		if c.Quality != "" {
			q := gen.VoiceConfigQuality(c.Quality)
			w.Quality = &q
		}
		out.Languages[lang] = w
	}
	for lang, lex := range v.Lexicon {
		out.Lexicon[lang] = lex
	}
	return out
}
