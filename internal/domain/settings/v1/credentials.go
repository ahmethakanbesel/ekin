package settings

import (
	"net/url"
	"strconv"
	"strings"

	"kurgu/internal/apperror"
)

// Credentials are the instance's API keys and service settings. Each
// value comes from the settings page when set there, else from its
// KURGU_* environment variable.
type Credentials struct {
	ClaudeOAuthToken    string `json:"claudeOauthToken,omitempty"`
	ClaudeModel         string `json:"claudeModel,omitempty"`
	PexelsAPIKey        string `json:"pexelsApiKey,omitempty"`
	OpenRouterAPIKey    string `json:"openrouterApiKey,omitempty"`
	DecisionModel       string `json:"decisionModel,omitempty"`
	SMTPHost            string `json:"smtpHost,omitempty"`
	SMTPPort            string `json:"smtpPort,omitempty"`
	SMTPUsername        string `json:"smtpUsername,omitempty"`
	SMTPPassword        string `json:"smtpPassword,omitempty"`
	SMTPFrom            string `json:"smtpFrom,omitempty"`
	SMTPTLS             string `json:"smtpTls,omitempty"`
	YouTubeClientID     string `json:"youtubeClientId,omitempty"`
	YouTubeClientSecret string `json:"youtubeClientSecret,omitempty"`
	OpenSEOAPIKey       string `json:"openseoApiKey,omitempty"`
	OpenSEOURL          string `json:"openseoUrl,omitempty"`
}

// CredentialField describes one credential for the settings page.
type CredentialField struct {
	Key    string // JSON name, e.g. pexelsApiKey
	Env    string // fallback variable
	Secret bool   // never returned once set
	Label  string
	get    func(*Credentials) *string
}

// Fields lists every credential in display order.
var Fields = []CredentialField{
	{Key: "claudeOauthToken", Env: "CLAUDE_CODE_OAUTH_TOKEN", Secret: true, Label: "Claude Code token (claude setup-token)", get: func(c *Credentials) *string { return &c.ClaudeOAuthToken }},
	{Key: "claudeModel", Env: "KURGU_CLAUDE_MODEL", Label: "Claude model", get: func(c *Credentials) *string { return &c.ClaudeModel }},
	{Key: "pexelsApiKey", Env: "KURGU_PEXELS_API_KEY", Secret: true, Label: "Pexels API key", get: func(c *Credentials) *string { return &c.PexelsAPIKey }},
	{Key: "openrouterApiKey", Env: "KURGU_OPENROUTER_API_KEY", Secret: true, Label: "OpenRouter API key", get: func(c *Credentials) *string { return &c.OpenRouterAPIKey }},
	{Key: "decisionModel", Env: "KURGU_DECISION_MODEL", Label: "Decision model", get: func(c *Credentials) *string { return &c.DecisionModel }},
	{Key: "smtpHost", Env: "KURGU_SMTP_HOST", Label: "SMTP host", get: func(c *Credentials) *string { return &c.SMTPHost }},
	{Key: "smtpPort", Env: "KURGU_SMTP_PORT", Label: "SMTP port", get: func(c *Credentials) *string { return &c.SMTPPort }},
	{Key: "smtpUsername", Env: "KURGU_SMTP_USERNAME", Label: "SMTP username", get: func(c *Credentials) *string { return &c.SMTPUsername }},
	{Key: "smtpPassword", Env: "KURGU_SMTP_PASSWORD", Secret: true, Label: "SMTP password", get: func(c *Credentials) *string { return &c.SMTPPassword }},
	{Key: "smtpFrom", Env: "KURGU_SMTP_FROM", Label: "SMTP from address", get: func(c *Credentials) *string { return &c.SMTPFrom }},
	{Key: "smtpTls", Env: "KURGU_SMTP_TLS", Label: "SMTP TLS (starttls, tls, none)", get: func(c *Credentials) *string { return &c.SMTPTLS }},
	{Key: "youtubeClientId", Env: "KURGU_YOUTUBE_CLIENT_ID", Label: "YouTube OAuth client ID", get: func(c *Credentials) *string { return &c.YouTubeClientID }},
	{Key: "youtubeClientSecret", Env: "KURGU_YOUTUBE_CLIENT_SECRET", Secret: true, Label: "YouTube OAuth client secret", get: func(c *Credentials) *string { return &c.YouTubeClientSecret }},
	{Key: "openseoApiKey", Env: "KURGU_OPENSEO_API_KEY", Secret: true, Label: "OpenSEO API key (oseo_…)", get: func(c *Credentials) *string { return &c.OpenSEOAPIKey }},
	{Key: "openseoUrl", Env: "KURGU_OPENSEO_URL", Label: "OpenSEO MCP endpoint", get: func(c *Credentials) *string { return &c.OpenSEOURL }},
}

// Value returns the field's value in c.
func (f CredentialField) Value(c Credentials) string { return *f.get(&c) }

// Merge returns base with every non-empty value of over applied.
func (c Credentials) Merge(over Credentials) Credentials {
	out := c
	for _, f := range Fields {
		if v := *f.get(&over); v != "" {
			*f.get(&out) = v
		}
	}
	return out
}

// Apply sets the values in patch (key → value; "" clears) and checks them.
func (c *Credentials) Apply(patch map[string]string) error {
	for key, v := range patch {
		var field *CredentialField
		for i := range Fields {
			if Fields[i].Key == key {
				field = &Fields[i]
			}
		}
		if field == nil {
			return apperror.New(apperror.ValidationFailed, "unknown credential "+key)
		}
		v = strings.TrimSpace(v)
		if len(v) > 4096 {
			return apperror.New(apperror.ValidationFailed, field.Label+" is too long")
		}
		*field.get(c) = v
	}
	if c.SMTPPort != "" {
		if p, err := strconv.Atoi(c.SMTPPort); err != nil || p < 1 || p > 65535 {
			return apperror.New(apperror.ValidationFailed, "SMTP port must be a number from 1 to 65535")
		}
	}
	if t := c.SMTPTLS; t != "" && t != "starttls" && t != "tls" && t != "none" {
		return apperror.New(apperror.ValidationFailed, "SMTP TLS must be starttls, tls or none")
	}
	if k := c.OpenSEOAPIKey; k != "" && !strings.HasPrefix(k, "oseo_") {
		return apperror.New(apperror.ValidationFailed, "an OpenSEO API key starts with oseo_")
	}
	if u := c.OpenSEOURL; u != "" {
		parsed, err := url.Parse(u)
		local := err == nil && parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1")
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !local) {
			return apperror.New(apperror.ValidationFailed, "the OpenSEO endpoint must be an https URL (http only for localhost)")
		}
	}
	return nil
}

// Port returns the SMTP port, 0 when unset.
func (c Credentials) Port() int {
	p, _ := strconv.Atoi(c.SMTPPort)
	return p
}

// Hint shows the last characters of a secret so people can tell keys
// apart without revealing them.
func Hint(v string) string {
	if len(v) <= 8 {
		return "••••"
	}
	return "••••" + v[len(v)-4:]
}
