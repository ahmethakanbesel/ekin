// Package openseo calls OpenSEO's tools directly over its MCP endpoint
// (JSON-RPC over HTTP POST, stateless) with an API key, without an agent.
package openseo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultURL is hosted OpenSEO's MCP endpoint.
const DefaultURL = "https://app.openseo.so/mcp"

const protocolVersion = "2025-06-18"

// Client calls OpenSEO tools.
type Client struct {
	URL    string
	APIKey string
	HTTP   *http.Client

	once    sync.Once
	initErr error
	nextID  atomic.Int64
}

// Error is a failed call: an HTTP error (bad key, rate limit, no
// credits) or a tool that reported an error.
type Error struct {
	Status     int    // HTTP status; 0 for a tool error
	Code       string // invalid_api_key, rate_limited, usage_exceeded, ...
	Message    string
	RetryAfter time.Duration // set with rate_limited
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return "OpenSEO: " + e.Message
	}
	return fmt.Sprintf("OpenSEO: %s (HTTP %d %s)", e.Message, e.Status, e.Code)
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     *int64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Call runs tool with args and decodes its structured result into out.
func (c *Client) Call(ctx context.Context, tool string, args, out any) error {
	c.once.Do(func() { c.initErr = c.initialize(ctx) })
	if c.initErr != nil {
		return c.initErr
	}
	if args == nil {
		args = map[string]any{}
	}
	raw, err := c.request(ctx, "tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return err
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("OpenSEO %s: %w", tool, err)
	}
	if res.IsError {
		var texts []string
		for _, c := range res.Content {
			texts = append(texts, c.Text)
		}
		return &Error{Message: tool + ": " + strings.TrimSpace(strings.Join(texts, " "))}
	}
	if out == nil {
		return nil
	}
	if len(res.Structured) == 0 {
		return fmt.Errorf("OpenSEO %s returned no structured content", tool)
	}
	return json.Unmarshal(res.Structured, out)
}

func (c *Client) initialize(ctx context.Context) error {
	_, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "ekin", "version": "1"},
	})
	if err != nil {
		return err
	}
	return c.notify(ctx, "notifications/initialized")
}

func (c *Client) notify(ctx context.Context, method string) error {
	resp, err := c.post(ctx, rpcRequest{JSONRPC: "2.0", Method: method})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

func (c *Client) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	resp, err := c.post(ctx, rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body := io.LimitReader(resp.Body, 32<<20)
	var msg rpcResponse
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		msg, err = readSSE(body, id)
	} else {
		err = json.NewDecoder(body).Decode(&msg)
	}
	if err != nil {
		return nil, fmt.Errorf("OpenSEO %s: %w", method, err)
	}
	if msg.Error != nil {
		return nil, &Error{Message: fmt.Sprintf("%s: %s (%d)", method, msg.Error.Message, msg.Error.Code)}
	}
	return msg.Result, nil
}

// readSSE returns the response with id from an event stream.
func readSSE(r io.Reader, id int64) (rpcResponse, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 32<<20)
	var data strings.Builder
	flush := func() (rpcResponse, bool) {
		defer data.Reset()
		var msg rpcResponse
		if data.Len() == 0 || json.Unmarshal([]byte(data.String()), &msg) != nil || msg.ID == nil || *msg.ID != id {
			return msg, false
		}
		return msg, true
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if msg, ok := flush(); ok {
				return msg, nil
			}
			continue
		}
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimPrefix(v, " "))
		}
	}
	if msg, ok := flush(); ok {
		return msg, nil
	}
	if err := sc.Err(); err != nil {
		return rpcResponse{}, err
	}
	return rpcResponse{}, errors.New("the event stream ended without a response")
}

func (c *Client) post(ctx context.Context, msg rpcRequest) (*http.Response, error) {
	if c.APIKey == "" {
		return nil, &Error{Status: http.StatusUnauthorized, Code: "no_api_key", Message: "no OpenSEO API key is set"}
	}
	raw, _ := json.Marshal(msg)
	url := c.URL
	if url == "" {
		url = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 3 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenSEO: %w", err)
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	e := &Error{Status: resp.StatusCode, Code: body.Error, Message: body.Description}
	if e.Message == "" {
		e.Message = http.StatusText(resp.StatusCode)
	}
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
		e.RetryAfter = time.Duration(s) * time.Second
	}
	return nil, e
}
