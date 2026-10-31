package agenttool_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/agenttool"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// changeDir is a work dir whose repository has a base commit.
func changeDir(t *testing.T) (dir, repo string) {
	t.Helper()
	dir = t.TempDir()
	repo = filepath.Join(dir, agenttool.RepoDir)
	write(t, repo, "index.html", "<title>Home</title>\n")
	write(t, repo, "about.html", "<title></title>\n")
	write(t, repo, "old.html", "gone soon\n")
	write(t, repo, ".gitignore", "dist/\nnode_modules/\n")
	if err := agenttool.InitBase(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	return dir, repo
}

func TestChangesDiffAndPack(t *testing.T) {
	ctx := context.Background()
	dir, repo := changeDir(t)
	if c, err := agenttool.Changes(ctx, repo); err != nil || len(c) != 0 {
		t.Fatalf("fresh copy changes = %v %v", c, err)
	}
	write(t, repo, "about.html", "<title>About us</title>\n")
	write(t, repo, "llms.txt", "# Site\n")
	write(t, repo, "dist/out.html", "build output")
	if err := os.Remove(filepath.Join(repo, "old.html")); err != nil {
		t.Fatal(err)
	}
	changes, err := agenttool.Changes(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range changes {
		got[c.Path] = c.Status
	}
	if len(got) != 3 || got["about.html"] != "modified" || got["llms.txt"] != "added" || got["old.html"] != "deleted" {
		t.Fatalf("changes = %+v", changes)
	}
	for _, c := range changes {
		if c.Path == "about.html" && (c.Additions != 1 || c.Deletions != 1) {
			t.Fatalf("about.html counts = %+v", c)
		}
	}
	patch, err := agenttool.Diff(ctx, repo)
	if err != nil || !bytes.Contains(patch, []byte("+<title>About us</title>")) || bytes.Contains(patch, []byte("dist/")) {
		t.Fatalf("patch = %s %v", patch, err)
	}
	var packed bytes.Buffer
	if err := agenttool.Pack(repo, changes, &packed); err != nil {
		t.Fatal(err)
	}
	files, _, err := agenttool.Unpack(&packed, 1<<20)
	if err != nil || len(files) != 2 || string(files["llms.txt"]) != "# Site\n" {
		t.Fatalf("unpacked = %v %v", files, err)
	}

	// The patch brings the change back on a fresh copy of the base.
	_, fresh := changeDir(t)
	if err := agenttool.ApplyPatch(ctx, fresh, patch); err != nil {
		t.Fatal(err)
	}
	again, _ := agenttool.Changes(ctx, fresh)
	if len(again) != 3 {
		t.Fatalf("re-applied changes = %+v", again)
	}
	_ = dir
}

func TestRunBuildCleansUpAndHidesSecrets(t *testing.T) {
	ctx := context.Background()
	dir, repo := changeDir(t)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "secret-token")
	write(t, repo, "about.html", "<title>About</title>\n")
	cmd := `env; mkdir -p dist && echo built > dist/out.html && echo stray > stray.txt && echo x >> index.html`
	res, err := agenttool.RunBuild(ctx, dir, agenttool.BuildSpec{Command: cmd})
	if err != nil || !res.OK || res.Tree == "" {
		t.Fatalf("build = %+v %v", res, err)
	}
	if strings.Contains(res.Output, "secret-token") || !strings.Contains(res.Output, "CI=true") {
		t.Fatalf("the build saw the wrong environment:\n%s", res.Output)
	}
	if _, err := os.Stat(filepath.Join(repo, "stray.txt")); !os.IsNotExist(err) {
		t.Fatal("a file the build made is left in the change")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "index.html")); string(b) != "<title>Home</title>\n" {
		t.Fatalf("a file the build changed is not put back: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "about.html")); string(b) != "<title>About</title>\n" {
		t.Fatalf("the agent's edit is lost: %q", b)
	}
	if tree, _ := agenttool.Tree(ctx, repo); tree != res.Tree {
		t.Fatalf("tree after the build %s, before %s", tree, res.Tree)
	}
	last, ok := agenttool.LastBuild(dir)
	if !ok || last.Tree != res.Tree || !last.OK {
		t.Fatalf("recorded build = %+v %v", last, ok)
	}

	failed, err := agenttool.RunBuild(ctx, dir, agenttool.BuildSpec{Command: "echo broken >&2; exit 3"})
	if err != nil || failed.OK || failed.ExitCode != 3 || !strings.Contains(failed.Output, "broken") {
		t.Fatalf("failed build = %+v %v", failed, err)
	}
	slow, err := agenttool.RunBuild(ctx, dir, agenttool.BuildSpec{Command: "sleep 30", TimeoutSeconds: 1})
	if err != nil || slow.OK || !strings.Contains(slow.Output, "stopped") {
		t.Fatalf("timed-out build = %+v %v", slow, err)
	}
}

func TestBuildAndDiffTools(t *testing.T) {
	ctx := context.Background()
	dir, repo := changeDir(t)
	var out bytes.Buffer
	e := agenttool.Env{Dir: dir, Stdout: &out, Stderr: &out,
		Spec: agenttool.Spec{Change: true, Build: &agenttool.BuildSpec{Command: "test -s about.html"}}}
	if err := agenttool.Run(ctx, e, []string{"diff"}); err != nil || !strings.Contains(out.String(), "No changes yet") {
		t.Fatalf("diff before edits: %v\n%s", err, out.String())
	}
	write(t, repo, "about.html", "<title>About</title>\n")
	out.Reset()
	if err := agenttool.Run(ctx, e, []string{"diff"}); err != nil || !strings.Contains(out.String(), "about.html") {
		t.Fatalf("diff: %v\n%s", err, out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, agenttool.PatchFile)); !bytes.Contains(b, []byte("About")) {
		t.Fatalf("change.patch = %s", b)
	}
	if err := agenttool.Run(ctx, e, []string{"build"}); err != nil {
		t.Fatalf("build: %v\n%s", err, out.String())
	}
	e.Spec.Build.Command = "exit 1"
	if err := agenttool.Run(ctx, e, []string{"build"}); err == nil {
		t.Fatal("a failing build passed")
	}
	e.Spec = agenttool.Spec{}
	if err := agenttool.Run(ctx, e, []string{"build"}); err == nil {
		t.Fatal("build ran outside a change")
	}
}
