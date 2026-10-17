package claude_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ekin/internal/adapter/claude"
)

// fake writes a shell script that records its arguments and prints out.
func fake(t *testing.T, out string, code int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> " + argsFile + "; done\npwd > " + dir + "/cwd\ncat <<'JSON'\n" + out + "\nJSON\nexit " + string(rune('0'+code)) + "\n"
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func TestRunSuccess(t *testing.T) {
	bin, argsFile := fake(t, `{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"s1","total_cost_usd":0.5,"num_turns":3,"structured_output":{"ok":true}}`, 0)
	work := t.TempDir()
	r := &claude.Runner{Bin: bin, Model: "sonnet", Effort: "low"}
	res, err := r.Run(context.Background(), claude.Request{
		Dir: work, Prompt: "write it", Tools: []string{"Read", "Write"},
		Allowed: []string{"Read", "Bash(/k tool:*)"}, Resume: "s0", JSONSchema: []byte(`{"type":"object"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "s1" || res.Text != "done" || string(res.Structured) != `{"ok":true}` || res.Turns != 3 {
		t.Fatalf("result = %+v", res)
	}
	raw, _ := os.ReadFile(argsFile)
	args := string(raw)
	for _, want := range []string{"-p\nwrite it\n", "--tools\nRead,Write\n", "--allowedTools\nRead,Bash(/k tool:*)\n", "--resume\ns0\n", "--model\nsonnet\n", "--effort\nlow\n", "--setting-sources\n\n"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q:\n%s", want, args)
		}
	}
}

// A request's model and effort win over the runner's.
func TestRunRequestOverridesModelAndEffort(t *testing.T) {
	bin, argsFile := fake(t, `{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"s1"}`, 0)
	r := &claude.Runner{Bin: bin, Model: "opus", Effort: "high"}
	if _, err := r.Run(context.Background(), claude.Request{Dir: t.TempDir(), Prompt: "p", Model: "claude-sonnet-5-5", Effort: "low"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(argsFile)
	args := string(raw)
	if !strings.Contains(args, "--model\nclaude-sonnet-5-5\n") || !strings.Contains(args, "--effort\nlow\n") ||
		strings.Contains(args, "\nopus\n") || strings.Contains(args, "\nhigh\n") {
		t.Fatalf("args:\n%s", args)
	}
}

func TestRunErrors(t *testing.T) {
	bin, _ := fake(t, `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"","session_id":"s2"}`, 1)
	_, err := (&claude.Runner{Bin: bin}).Run(context.Background(), claude.Request{Dir: t.TempDir(), Prompt: "x"})
	var ce *claude.Error
	if !errors.As(err, &ce) || ce.Subtype != "error_max_turns" || ce.SessionID != "s2" {
		t.Fatalf("err = %v", err)
	}
	bin, _ = fake(t, `not json`, 1)
	if _, err := (&claude.Runner{Bin: bin}).Run(context.Background(), claude.Request{Dir: t.TempDir(), Prompt: "x"}); err == nil {
		t.Fatal("garbage accepted")
	}
	bin, _ = fake(t, `{"type":"result","subtype":"success","result":"plain"}`, 0)
	_, err = (&claude.Runner{Bin: bin}).Run(context.Background(), claude.Request{Dir: t.TempDir(), Prompt: "x", JSONSchema: []byte(`{}`)})
	if !errors.As(err, &ce) || ce.Subtype != "no_structured_output" {
		t.Fatalf("missing structured output err = %v", err)
	}
}
