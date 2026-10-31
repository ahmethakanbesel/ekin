package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Answer is a model's reply to a question as a person would ask it.
type Answer struct {
	Text string
	// Citations are the URLs the answer cites: the web search's
	// annotations, then any other link in the text, each once.
	Citations []string
	// Cost is what OpenRouter billed for the call, in US dollars; 0 when
	// it did not say.
	Cost float64
}

var linkPattern = regexp.MustCompile(`https?://[^\s<>()\[\]"'` + "`" + `]+`)

// Ask sends prompt to model as a user message, with no system prompt,
// the way someone asking an assistant would. A model id ending in
// ":online" searches the web first, and its sources come back as
// citations.
func (c *Client) Ask(ctx context.Context, model, prompt string) (Answer, error) {
	if !c.Enabled() {
		return Answer{}, errors.New("openrouter: no API key")
	}
	base := c.BaseURL
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	body, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
		"usage":    map[string]any{"include": true},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "Ekin")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 3 * time.Minute}
	}
	res, err := hc.Do(req) //nolint:gosec // the configured OpenRouter API
	if err != nil {
		return Answer{}, fmt.Errorf("openrouter: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return Answer{}, fmt.Errorf("openrouter: %s answered %d: %s", model, res.StatusCode, firstN(string(raw), 300))
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content     string `json:"content"`
				Annotations []struct {
					Type        string `json:"type"`
					URLCitation struct {
						URL string `json:"url"`
					} `json:"url_citation"`
				} `json:"annotations"`
			} `json:"message"`
		} `json:"choices"`
		Citations []string `json:"citations"` // some providers (Perplexity) list them here
		Usage     struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Answer{}, fmt.Errorf("openrouter: unexpected response: %s", firstN(string(raw), 300))
	}
	if resp.Error != nil {
		return Answer{}, fmt.Errorf("openrouter: %s: %s", model, resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return Answer{}, fmt.Errorf("openrouter: %s returned no answer", model)
	}
	msg := resp.Choices[0].Message
	out := Answer{Text: strings.TrimSpace(msg.Content), Cost: resp.Usage.Cost}
	seen := map[string]bool{}
	add := func(u string) {
		u = strings.TrimRight(u, ".,;:!?")
		if u != "" && !seen[u] {
			seen[u] = true
			out.Citations = append(out.Citations, u)
		}
	}
	for _, a := range msg.Annotations {
		if a.Type == "url_citation" {
			add(a.URLCitation.URL)
		}
	}
	for _, u := range resp.Citations {
		add(u)
	}
	for _, u := range linkPattern.FindAllString(out.Text, -1) {
		add(u)
	}
	return out, nil
}
