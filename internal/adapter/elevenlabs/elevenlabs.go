// Package elevenlabs calls the ElevenLabs API: the account's voices and
// models, its character usage, and speech with per-character timings.
package elevenlabs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// DefaultURL is the ElevenLabs API.
const DefaultURL = "https://api.elevenlabs.io"

// SampleRate is the rate of the PCM Speak returns. 44.1 kHz PCM needs a
// Pro plan; 24 kHz works on every plan.
const SampleRate = 24000

// ErrNoKey means no API key is configured.
var ErrNoKey = errors.New("elevenlabs: no API key (set it under System → Credentials)")

// Client calls ElevenLabs.
type Client struct {
	APIKey  string
	BaseURL string // default DefaultURL
	HTTP    *http.Client
	// Retry is the wait before retrying a rate-limited or overloaded
	// request once; default 2 s.
	Retry time.Duration
}

// Error is an API error.
type Error struct {
	Status  int
	Code    string // e.g. quota_exceeded, voice_not_found
	Message string
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Code != "" {
		return fmt.Sprintf("elevenlabs: %s (%s, HTTP %d)", msg, e.Code, e.Status)
	}
	return fmt.Sprintf("elevenlabs: %s (HTTP %d)", msg, e.Status)
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultURL
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 3 * time.Minute}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if c.APIKey == "" {
		return ErrNoKey
	}
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.base()+path, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("xi-api-key", c.APIKey)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := c.http().Do(req)
		if err != nil {
			return fmt.Errorf("elevenlabs: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
		_ = res.Body.Close()
		if err != nil {
			return fmt.Errorf("elevenlabs: %w", err)
		}
		if res.StatusCode == http.StatusOK {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("elevenlabs: unexpected response: %w", err)
			}
			return nil
		}
		apiErr := parseError(res.StatusCode, data)
		retryable := res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable
		if !retryable || attempt > 0 || apiErr.Code == "quota_exceeded" {
			return apiErr
		}
		wait := c.Retry
		if wait == 0 {
			wait = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// parseError reads {"detail": {"status": ..., "message": ...}}, the
// validation form {"detail": [{"msg": ...}]} or a bare {"detail": "..."}.
func parseError(status int, data []byte) *Error {
	e := &Error{Status: status}
	var env struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(data, &env) != nil || len(env.Detail) == 0 {
		return e
	}
	var obj struct {
		Status  string `json:"status"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	var list []struct {
		Msg string `json:"msg"`
	}
	var text string
	switch {
	case json.Unmarshal(env.Detail, &obj) == nil && (obj.Message != "" || obj.Status != ""):
		e.Code, e.Message = obj.Status, obj.Message
		if e.Code == "" {
			e.Code = obj.Code
		}
	case json.Unmarshal(env.Detail, &list) == nil && len(list) > 0:
		msgs := make([]string, 0, len(list))
		for _, l := range list {
			msgs = append(msgs, l.Msg)
		}
		e.Message = strings.Join(msgs, "; ")
	case json.Unmarshal(env.Detail, &text) == nil:
		e.Message = text
	}
	return e
}

// Voice is one voice of the account.
type Voice struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Category   string            `json:"category"` // premade, cloned, generated, professional
	Labels     map[string]string `json:"labels"`   // accent, gender, age, use_case, ...
	PreviewURL string            `json:"previewUrl"`
	Languages  []string          `json:"languages"` // verified languages, e.g. tr, en
}

// Voices lists the account's voices, at most limit.
func (c *Client) Voices(ctx context.Context, limit int) ([]Voice, error) {
	var out []Voice
	token := ""
	for len(out) < limit {
		q := url.Values{"page_size": {"100"}, "sort": {"name"}, "sort_direction": {"asc"}}
		if token != "" {
			q.Set("next_page_token", token)
		}
		var page struct {
			Voices []struct {
				VoiceID           string            `json:"voice_id"`
				Name              string            `json:"name"`
				Category          string            `json:"category"`
				Labels            map[string]string `json:"labels"`
				PreviewURL        string            `json:"preview_url"`
				VerifiedLanguages []struct {
					Language string `json:"language"`
				} `json:"verified_languages"`
			} `json:"voices"`
			HasMore       bool   `json:"has_more"`
			NextPageToken string `json:"next_page_token"`
		}
		if err := c.do(ctx, http.MethodGet, "/v2/voices?"+q.Encode(), nil, &page); err != nil {
			return nil, err
		}
		for _, v := range page.Voices {
			langs := []string{}
			for _, l := range v.VerifiedLanguages {
				if l.Language != "" && !slices.Contains(langs, l.Language) {
					langs = append(langs, l.Language)
				}
			}
			out = append(out, Voice{ID: v.VoiceID, Name: v.Name, Category: v.Category, Labels: v.Labels,
				PreviewURL: v.PreviewURL, Languages: langs})
		}
		if !page.HasMore || page.NextPageToken == "" {
			break
		}
		token = page.NextPageToken
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Model is a text-to-speech model.
type Model struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Languages       []string `json:"languages"` // language IDs, e.g. tr, en
	MaxCharacters   int      `json:"maxCharacters"`
	CostMultiplier  float64  `json:"costMultiplier"` // credits per character
	CanStyle        bool     `json:"canStyle"`
	CanSpeakerBoost bool     `json:"canSpeakerBoost"`
}

// Models lists the models that can do text to speech.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var raw []struct {
		ModelID                      string  `json:"model_id"`
		Name                         string  `json:"name"`
		Description                  string  `json:"description"`
		CanDoTextToSpeech            bool    `json:"can_do_text_to_speech"`
		CanUseStyle                  bool    `json:"can_use_style"`
		CanUseSpeakerBoost           bool    `json:"can_use_speaker_boost"`
		MaximumTextLengthPerRequest  int     `json:"maximum_text_length_per_request"`
		MaxCharactersRequestSubsUser int     `json:"max_characters_request_subscribed_user"`
		TokenCostFactor              float64 `json:"token_cost_factor"`
		Languages                    []struct {
			LanguageID string `json:"language_id"`
		} `json:"languages"`
		ModelRates *struct {
			CharacterCostMultiplier float64 `json:"character_cost_multiplier"`
		} `json:"model_rates"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/models", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(raw))
	for _, m := range raw {
		if !m.CanDoTextToSpeech {
			continue
		}
		mm := Model{ID: m.ModelID, Name: m.Name, Description: m.Description, MaxCharacters: m.MaximumTextLengthPerRequest,
			CostMultiplier: m.TokenCostFactor, CanStyle: m.CanUseStyle, CanSpeakerBoost: m.CanUseSpeakerBoost, Languages: []string{}}
		if mm.MaxCharacters == 0 {
			mm.MaxCharacters = m.MaxCharactersRequestSubsUser
		}
		if m.ModelRates != nil && m.ModelRates.CharacterCostMultiplier > 0 {
			mm.CostMultiplier = m.ModelRates.CharacterCostMultiplier
		}
		for _, l := range m.Languages {
			mm.Languages = append(mm.Languages, l.LanguageID)
		}
		out = append(out, mm)
	}
	return out, nil
}

// Subscription is the account's plan and character usage.
type Subscription struct {
	Tier           string `json:"tier"`
	Status         string `json:"status"`
	CharacterCount int    `json:"characterCount"`
	CharacterLimit int    `json:"characterLimit"`
	ResetUnix      int64  `json:"resetUnix,omitempty"`
}

// Subscription returns the plan and usage. A key restricted to text to
// speech may not read it (HTTP 401 missing_permissions).
func (c *Client) Subscription(ctx context.Context) (Subscription, error) {
	var raw struct {
		Tier                        string `json:"tier"`
		Status                      string `json:"status"`
		CharacterCount              int    `json:"character_count"`
		CharacterLimit              int    `json:"character_limit"`
		NextCharacterCountResetUnix int64  `json:"next_character_count_reset_unix"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/user/subscription", nil, &raw); err != nil {
		return Subscription{}, err
	}
	return Subscription{Tier: raw.Tier, Status: raw.Status, CharacterCount: raw.CharacterCount,
		CharacterLimit: raw.CharacterLimit, ResetUnix: raw.NextCharacterCountResetUnix}, nil
}

// VoiceSettings shape one request's delivery.
type VoiceSettings struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	Style           float64 `json:"style"`
	UseSpeakerBoost bool    `json:"use_speaker_boost"`
	Speed           float64 `json:"speed"`
}

// SpeakRequest is one text to synthesise.
type SpeakRequest struct {
	Text  string
	Model string
	// Language is an ISO 639-1 code; it is sent only to models that
	// accept it.
	Language string
	Settings VoiceSettings
	Seed     int
	// PreviousText and NextText are the neighbouring chunks, so a long
	// narration read in pieces keeps one intonation.
	PreviousText, NextText string
}

// Speech is synthesised audio with the start and end of every character
// of the request's text, in seconds.
type Speech struct {
	PCM        []int16 // mono, SampleRate
	Characters []string
	Starts     []float64
	Ends       []float64
}

// Speak synthesises r.Text with voiceID.
func (c *Client) Speak(ctx context.Context, voiceID string, r SpeakRequest) (Speech, error) {
	body := map[string]any{"text": r.Text, "model_id": r.Model, "voice_settings": r.Settings}
	if r.Language != "" && !strings.HasPrefix(r.Model, "eleven_multilingual") {
		body["language_code"] = r.Language
	}
	if r.Seed > 0 {
		body["seed"] = r.Seed
	}
	if r.PreviousText != "" {
		body["previous_text"] = r.PreviousText
	}
	if r.NextText != "" {
		body["next_text"] = r.NextText
	}
	var res struct {
		AudioBase64 string `json:"audio_base64"`
		Alignment   *struct {
			Characters []string  `json:"characters"`
			Starts     []float64 `json:"character_start_times_seconds"`
			Ends       []float64 `json:"character_end_times_seconds"`
		} `json:"alignment"`
	}
	path := "/v1/text-to-speech/" + url.PathEscape(voiceID) + "/with-timestamps?output_format=pcm_" + fmt.Sprint(SampleRate)
	if err := c.do(ctx, http.MethodPost, path, body, &res); err != nil {
		return Speech{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(res.AudioBase64)
	if err != nil {
		return Speech{}, fmt.Errorf("elevenlabs: audio: %w", err)
	}
	if len(raw) == 0 || res.Alignment == nil {
		return Speech{}, errors.New("elevenlabs: the response has no audio or timings")
	}
	a := res.Alignment
	if len(a.Starts) != len(a.Characters) || len(a.Ends) != len(a.Characters) {
		return Speech{}, errors.New("elevenlabs: the timings do not match the characters")
	}
	pcm := make([]int16, len(raw)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(raw[2*i:])) //nolint:gosec // two's complement samples
	}
	return Speech{PCM: pcm, Characters: a.Characters, Starts: a.Starts, Ends: a.Ends}, nil
}
