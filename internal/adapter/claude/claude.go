// Package claude runs Claude Code headless (`claude -p`) as Kurgu's
// authoring agent. Each call is one non-interactive session in a working
// directory with an explicit tool allow-list; a later call can resume the
// session to apply review feedback with the earlier context intact.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner invokes the claude binary.
type Runner struct {
	Bin   string // default "claude"
	Model string // optional --model
	// Env is added to the inherited environment (CLAUDE_CODE_OAUTH_TOKEN
	// on a server, tool locations for `kurgu tool`).
	Env []string
}

// Request is one agent call.
type Request struct {
	Dir    string // working directory; edits stay inside it
	Prompt string
	// AppendSystemPrompt adds standing instructions.
	AppendSystemPrompt string
	// Tools are the built-in tools the session may use at all, e.g.
	// Read, Write, Edit, Glob, Grep, Bash. Empty means no tools.
	Tools []string
	// Allowed are pre-approved tool patterns, e.g. "Bash(/usr/bin/kurgu tool:*)".
	// Anything else that needs permission is denied (headless).
	Allowed []string
	// Resume continues an earlier session.
	Resume string
	// JSONSchema asks for structured output validated against it.
	JSONSchema json.RawMessage
	// Env is added to the runner's environment for this call.
	Env     []string
	Timeout time.Duration
}

// Result is what the session returned.
type Result struct {
	SessionID  string
	Text       string
	Structured json.RawMessage
	CostUSD    float64
	Turns      int
	Duration   time.Duration
}

type output struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	IsError    bool            `json:"is_error"`
	Result     string          `json:"result"`
	SessionID  string          `json:"session_id"`
	CostUSD    float64         `json:"total_cost_usd"`
	NumTurns   int             `json:"num_turns"`
	DurationMS int64           `json:"duration_ms"`
	Structured json.RawMessage `json:"structured_output"`
	Denials    []struct {
		ToolName string `json:"tool_name"`
	} `json:"permission_denials"`
}

// Error is a session that ran but did not succeed.
type Error struct {
	Subtype   string
	Message   string
	SessionID string
}

func (e *Error) Error() string {
	return fmt.Sprintf("claude: %s: %s", e.Subtype, e.Message)
}

// Run executes req and parses the JSON result.
func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	bin := r.Bin
	if bin == "" {
		bin = "claude"
	}
	args := []string{"-p", req.Prompt,
		"--output-format", "json",
		"--permission-mode", "acceptEdits",
		// User and project settings on the host (hooks, plugins, MCP
		// servers) must not leak into unattended runs.
		"--setting-sources", "",
		"--strict-mcp-config",
		"--tools", strings.Join(req.Tools, ","),
	}
	if len(req.Allowed) > 0 {
		args = append(args, "--allowedTools", strings.Join(req.Allowed, ","))
	}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	if req.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.AppendSystemPrompt)
	}
	if req.Resume != "" {
		args = append(args, "--resume", req.Resume)
	}
	if len(req.JSONSchema) > 0 {
		args = append(args, "--json-schema", string(req.JSONSchema))
	}
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // fixed binary, arguments built here
	cmd.Dir = req.Dir
	cmd.Env = append(append(os.Environ(), r.Env...), req.Env...)
	cmd.WaitDelay = 10 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	var out output
	if err := json.Unmarshal(lastJSONLine(stdout.Bytes()), &out); err != nil {
		if ctx.Err() != nil {
			return Result{}, fmt.Errorf("claude: %w", ctx.Err())
		}
		if errors.Is(runErr, exec.ErrNotFound) {
			return Result{}, errors.New("claude: the claude CLI is not installed")
		}
		return Result{}, fmt.Errorf("claude: no result (%w): %s", runErr, tail(stderr.String(), 2000))
	}
	res := Result{
		SessionID: out.SessionID, Text: out.Result, Structured: out.Structured,
		CostUSD: out.CostUSD, Turns: out.NumTurns, Duration: time.Duration(out.DurationMS) * time.Millisecond,
	}
	if out.IsError || out.Subtype != "success" {
		msg := out.Result
		if msg == "" {
			msg = tail(stderr.String(), 2000)
		}
		return res, &Error{Subtype: out.Subtype, Message: msg, SessionID: out.SessionID}
	}
	if len(req.JSONSchema) > 0 && len(out.Structured) == 0 {
		return res, &Error{Subtype: "no_structured_output", Message: tail(out.Result, 500), SessionID: out.SessionID}
	}
	return res, nil
}

func lastJSONLine(b []byte) []byte {
	b = bytes.TrimSpace(b)
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return b[i+1:]
	}
	return b
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}
