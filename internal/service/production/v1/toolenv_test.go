package production

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"kurgu/internal/agenttool"
)

// The agent's shell runs in the deliverable's own directory, so the
// directory holding the kurgu link must be on its PATH as an absolute
// path even when the work root is relative (KURGU_DATA_DIR=data).
func TestToolEnvPutsKurguOnAnAbsolutePath(t *testing.T) {
	tests := []struct {
		name, workRoot string
	}{
		{"relative work root", filepath.Join("data", "work")},
		{"absolute work root", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			root := tt.workRoot
			if root == "" {
				root = t.TempDir()
			}
			s := &Service{cfg: Config{WorkRoot: root, KurguBin: "/usr/bin/true"}, log: slog.New(slog.DiscardHandler)}
			var path string
			for _, kv := range s.toolEnv(context.Background(), agenttool.Spec{}) {
				if v, ok := strings.CutPrefix(kv, "PATH="); ok {
					path = v
				}
			}
			first, _, _ := strings.Cut(path, string(os.PathListSeparator))
			if !filepath.IsAbs(first) {
				t.Fatalf("PATH starts with %q, not an absolute directory", first)
			}
			// From the deliverable's directory, as the agent's shell runs.
			if err := os.MkdirAll(filepath.Join(root, "d1"), 0o750); err != nil {
				t.Fatal(err)
			}
			t.Chdir(filepath.Join(root, "d1"))
			if _, err := os.Stat(filepath.Join(first, "kurgu")); err != nil {
				t.Fatalf("no kurgu link reachable from the work dir: %v", err)
			}
		})
	}
}

// A shell that starts without the kurgu directory on PATH (a resumed
// session's) still finds kurgu through the env file Claude Code sources,
// and kurgu's absolute paths are pre-approved for an agent that falls
// back to them.
func TestKurguReachableWithoutPATH(t *testing.T) {
	t.Chdir(t.TempDir())
	s := &Service{cfg: Config{WorkRoot: filepath.Join("data", "work"), KurguBin: "/usr/bin/true"}, log: slog.New(slog.DiscardHandler)}
	var envFile string
	for _, kv := range s.toolEnv(context.Background(), agenttool.Spec{}) {
		if v, ok := strings.CutPrefix(kv, "CLAUDE_ENV_FILE="); ok {
			envFile = v
		}
	}
	if !filepath.IsAbs(envFile) {
		t.Fatalf("CLAUDE_ENV_FILE = %q", envFile)
	}
	if err := os.MkdirAll(filepath.Join("data", "work", "d1"), 0o750); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", `. "$1" && command -v kurgu`, "sh", envFile)
	cmd.Dir = filepath.Join("data", "work", "d1")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := cmd.Output()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "/.bin/kurgu") {
		t.Fatalf("command -v kurgu = %q, %v", out, err)
	}

	allowed := s.kurguAllowed(context.Background())
	link := filepath.Join(filepath.Dir(envFile), "kurgu")
	for _, want := range []string{"Bash(kurgu tool:*)", "Bash(" + link + " tool:*)", "Bash(/usr/bin/true tool:*)"} {
		if !slices.Contains(allowed, want) {
			t.Errorf("allowed %v lacks %q", allowed, want)
		}
	}
}
