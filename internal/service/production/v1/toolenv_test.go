package production

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
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
