// Package openrouter makes typed decisions with a small model on
// OpenRouter (default typesafe/jev-1.13): JSON in, JSON-schema-checked
// JSON out. Ekin uses it only for yes/no judgements such as "is this
// idea a repeat?", never for writing.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	storydomain "ekin/internal/domain/story/v1"
	storyrepo "ekin/internal/repository/story/v1"
)

// Client calls the chat completions API.
type Client struct {
	APIKey  string
	Model   string // default typesafe/jev-1.13
	BaseURL string // default https://openrouter.ai/api/v1
	HTTP    *http.Client
}

// Enabled reports whether an API key is configured.
func (c *Client) Enabled() bool { return c != nil && c.APIKey != "" }

// Decide sends system and user prompts and decodes the structured answer
// into out, which schema describes.
func (c *Client) Decide(ctx context.Context, system, user string, schemaName string, schema json.RawMessage, out any) error {
	if !c.Enabled() {
		return errors.New("openrouter: no API key")
	}
	model := c.Model
	if model == "" {
		model = "typesafe/jev-1.13"
	}
	base := c.BaseURL
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0,
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": schemaName, "strict": true, "schema": schema},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "Ekin")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("openrouter: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("openrouter: answered %d: %s", res.StatusCode, firstN(string(raw), 300))
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Choices) == 0 {
		return fmt.Errorf("openrouter: unexpected response: %s", firstN(string(raw), 300))
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	content = strings.TrimPrefix(strings.TrimSuffix(strings.TrimPrefix(content, "```json"), "```"), "```")
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), out); err != nil {
		return fmt.Errorf("openrouter: answer is not the requested JSON: %s", firstN(content, 200))
	}
	return nil
}

var duplicateSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["duplicate", "reason"],
  "properties": {
    "duplicate": {"type": "boolean"},
    "reason": {"type": "string"}
  }
}`)

// Dedupe implements the production Dedupe port.
type Dedupe struct{ Client *Client }

// Duplicate asks whether candidate covers the same ground as one of the
// earlier stories.
func (d Dedupe) Duplicate(ctx context.Context, candidate storydomain.Idea, similar []storyrepo.Match) (bool, string, error) {
	if !d.Client.Enabled() {
		return false, "", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Candidate:\n- title: %s\n- angle: %s\n\nEarlier stories:\n", candidate.Title, candidate.Angle)
	for _, m := range similar {
		fmt.Fprintf(&b, "- title: %s\n  angle: %s\n", m.Title, m.Angle)
	}
	var out struct {
		Duplicate bool   `json:"duplicate"`
		Reason    string `json:"reason"`
	}
	err := d.Client.Decide(ctx,
		"You judge whether a content idea repeats earlier content for the same audience. "+
			"It is a duplicate only if a viewer of an earlier piece would learn essentially nothing new: same question, same takeaway. "+
			"A different angle, audience segment or depth on the same topic is NOT a duplicate. Answer in JSON.",
		b.String(), "duplicate_verdict", duplicateSchema, &out)
	if err != nil {
		return false, "", err
	}
	return out.Duplicate, out.Reason, nil
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
