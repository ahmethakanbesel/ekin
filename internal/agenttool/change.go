package agenttool

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A change's work dir holds the site's repository in RepoDir, committed
// once as the base it was copied at, so the change is whatever differs
// from that commit.
const (
	RepoDir = "repo"
	// PatchFile is the change as a patch, written by `ekin tool diff`
	// and again when the draft is checked.
	PatchFile = "change.patch"
	// SummaryFile is the agent's account of the change for the reviewer.
	SummaryFile = "summary.md"
	buildFile   = ".ekin/build.json"
	homeDir     = ".ekin/home"
)

// Change limits.
const (
	MaxChangedFiles = 200
	MaxPatchBytes   = 4 << 20
	// buildOutputTail is how much of a build's output is kept and shown.
	buildOutputTail = 16 << 10
	// DefaultBuildTimeout bounds one build.
	DefaultBuildTimeout = 15 * time.Minute
)

// BuildSpec is how a change's site is built.
type BuildSpec struct {
	Command string `json:"command"`
	// TimeoutSeconds bounds one build; 0 means DefaultBuildTimeout.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

func (b BuildSpec) timeout() time.Duration {
	if b.TimeoutSeconds > 0 {
		return time.Duration(b.TimeoutSeconds) * time.Second
	}
	return DefaultBuildTimeout
}

// BuildResult is one run of the site's build over the repository's
// state Tree (the git tree of the work, before the build ran).
type BuildResult struct {
	Tree     string    `json:"tree"`
	OK       bool      `json:"ok"`
	ExitCode int       `json:"exitCode"`
	Seconds  float64   `json:"seconds"`
	Output   string    `json:"output"` // the tail
	At       time.Time `json:"at"`
}

// ChangedFile is one file the change touches.
type ChangedFile struct {
	Path       string `json:"path"`
	Status     string `json:"status"` // added, modified or deleted
	Executable bool   `json:"executable,omitempty"`
	Additions  int    `json:"additions"`
	Deletions  int    `json:"deletions"`
	Binary     bool   `json:"binary,omitempty"`
}

// git runs git in repo, isolated from the host's git configuration and
// hooks.
func git(ctx context.Context, repo string, extraEnv []string, stdin io.Reader, args ...string) ([]byte, error) {
	base := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.autocrlf=false", "-c", "commit.gpgsign=false",
		"-c", "user.name=Ekin", "-c", "user.email=ekin@localhost", "-c", "core.quotepath=false"}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...) //nolint:gosec // git with Ekin's own arguments
	cmd.Dir = repo
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + repo, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}, extraEnv...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// InitBase commits repo's files as the base a change is measured from.
func InitBase(ctx context.Context, repo string) error {
	if _, err := git(ctx, repo, nil, nil, "init", "-q"); err != nil {
		return err
	}
	if _, err := git(ctx, repo, nil, nil, "add", "-A"); err != nil {
		return err
	}
	_, err := git(ctx, repo, nil, nil, "commit", "-q", "--allow-empty", "--no-verify", "-m", "base")
	return err
}

// ApplyPatch applies patch (from Diff) onto repo's base, to bring back a
// change on a fresh copy.
func ApplyPatch(ctx context.Context, repo string, patch []byte) error {
	if len(bytes.TrimSpace(patch)) == 0 {
		return nil
	}
	_, err := git(ctx, repo, nil, bytes.NewReader(patch), "apply", "--binary", "--whitespace=nowarn", "-")
	return err
}

// Tree stages everything in repo and returns the tree it makes.
func Tree(ctx context.Context, repo string) (string, error) {
	if _, err := git(ctx, repo, nil, nil, "add", "-A"); err != nil {
		return "", err
	}
	out, err := git(ctx, repo, nil, nil, "write-tree")
	return strings.TrimSpace(string(out)), err
}

// BaseTree is the tree of the base commit.
func BaseTree(ctx context.Context, repo string) (string, error) {
	out, err := git(ctx, repo, nil, nil, "rev-parse", "HEAD^{tree}")
	return strings.TrimSpace(string(out)), err
}

// Diff is the change against the base, as a binary-safe patch.
func Diff(ctx context.Context, repo string) ([]byte, error) {
	if _, err := Tree(ctx, repo); err != nil {
		return nil, err
	}
	return git(ctx, repo, nil, nil, "diff", "--cached", "--binary", "--no-renames", "--no-color", "HEAD")
}

// Changes lists the files that differ from the base.
func Changes(ctx context.Context, repo string) ([]ChangedFile, error) {
	if _, err := Tree(ctx, repo); err != nil {
		return nil, err
	}
	raw, err := git(ctx, repo, nil, nil, "diff", "--cached", "--raw", "--no-renames", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	numstat, err := git(ctx, repo, nil, nil, "diff", "--cached", "--numstat", "--no-renames", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	counts := map[string][3]int{} // additions, deletions, binary
	for rec := range strings.SplitSeq(string(numstat), "\x00") {
		f := strings.SplitN(rec, "\t", 3)
		if len(f) != 3 {
			continue
		}
		add, errA := strconv.Atoi(f[0])
		del, errD := strconv.Atoi(f[1])
		bin := 0
		if errA != nil || errD != nil {
			bin = 1
		}
		counts[f[2]] = [3]int{add, del, bin}
	}
	var out []ChangedFile
	parts := strings.Split(string(raw), "\x00")
	for i := 0; i+1 < len(parts); i += 2 {
		meta, p := strings.Fields(strings.TrimPrefix(parts[i], ":")), parts[i+1]
		if len(meta) < 5 {
			continue
		}
		newMode, status := meta[1], meta[4]
		if newMode == "120000" || newMode == "160000" || meta[0] == "120000" || meta[0] == "160000" {
			return nil, fmt.Errorf("%s: symbolic links and submodules cannot be changed", p)
		}
		cf := ChangedFile{Path: p, Executable: newMode == "100755"}
		switch status[:1] {
		case "A":
			cf.Status = "added"
		case "D":
			cf.Status, cf.Executable = "deleted", false
		default:
			cf.Status = "modified"
		}
		c := counts[p]
		cf.Additions, cf.Deletions, cf.Binary = c[0], c[1], c[2] == 1
		out = append(out, cf)
	}
	return out, nil
}

// Pack writes the added and modified files of changes, as repo holds
// them, as a gzipped tar of repository paths.
func Pack(repo string, changes []ChangedFile, w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, c := range changes {
		if c.Status == "deleted" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(c.Path))) //nolint:gosec // inside the work dir
		if err != nil {
			return err
		}
		mode := int64(0o644)
		if c.Executable {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: c.Path, Mode: mode, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(b); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Unpack reads Pack's archive: repository path → content and whether
// it is executable. Paths that would leave the repository are refused.
func Unpack(r io.Reader, limit int64) (map[string][]byte, map[string]bool, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, nil, err
	}
	tr := tar.NewReader(gz)
	files, exec := map[string][]byte{}, map[string]bool{}
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return files, exec, nil
		}
		if err != nil {
			return nil, nil, err
		}
		name := path.Clean(h.Name)
		if h.Typeflag != tar.TypeReg || name == "." || strings.HasPrefix(name, "../") || path.IsAbs(name) || name == ".git" ||
			strings.HasPrefix(name, ".git/") {
			return nil, nil, fmt.Errorf("unsafe entry %q in the change", h.Name)
		}
		total += h.Size
		if total > limit {
			return nil, nil, errors.New("the change's files are too large")
		}
		b, err := io.ReadAll(io.LimitReader(tr, h.Size))
		if err != nil {
			return nil, nil, err
		}
		files[name], exec[name] = b, h.Mode&0o111 != 0
	}
}

// RunBuild runs spec's command at repo's root in dir's change, with no
// secrets in its environment, records the result, and puts the
// repository back exactly as it was before the build, so build output
// never becomes part of the change. out receives the output's tail.
func RunBuild(ctx context.Context, dir string, spec BuildSpec) (BuildResult, error) {
	repo := filepath.Join(dir, RepoDir)
	tree, err := Tree(ctx, repo)
	if err != nil {
		return BuildResult{}, err
	}
	home := filepath.Join(dir, homeDir)
	if err := os.MkdirAll(home, 0o750); err != nil {
		return BuildResult{}, err
	}
	bctx, cancel := context.WithTimeout(ctx, spec.timeout())
	defer cancel()
	// The site's build command, as an owner set it in the site settings:
	// running it is the point, in a scrubbed environment.
	cmd := exec.CommandContext(bctx, "/bin/sh", "-c", spec.Command) //nolint:gosec // see above
	cmd.Dir = repo
	// Only what a build needs: never the agent's credentials or Ekin's
	// settings.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "LANG=C.UTF-8", "CI=true", "TERM=dumb",
		"NO_COLOR=1", "TMPDIR=" + home}
	tail := &tailBuffer{max: buildOutputTail}
	cmd.Stdout, cmd.Stderr = tail, tail
	killGroup(cmd)
	start := time.Now()
	runErr := cmd.Run()
	res := BuildResult{Tree: tree, OK: runErr == nil, Seconds: time.Since(start).Round(100 * time.Millisecond).Seconds(),
		Output: tail.String(), At: time.Now().UTC()}
	if runErr != nil {
		res.ExitCode = -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			res.ExitCode = ee.ExitCode()
		}
		if bctx.Err() == context.DeadlineExceeded {
			res.Output += fmt.Sprintf("\n[ekin: the build ran longer than %s and was stopped]", spec.timeout())
		}
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if err := restore(ctx, repo, tree, filepath.Join(dir, ".ekin", "restore.index")); err != nil {
		return res, fmt.Errorf("put the repository back after the build: %w", err)
	}
	raw, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, buildFile), raw, 0o600); err != nil {
		return res, err
	}
	return res, nil
}

// restore makes repo's files exactly tree again (ignored files aside).
func restore(ctx context.Context, repo, tree, index string) error {
	env := []string{"GIT_INDEX_FILE=" + index}
	defer func() { _ = os.Remove(index) }()
	if _, err := git(ctx, repo, env, nil, "read-tree", tree); err != nil {
		return err
	}
	if _, err := git(ctx, repo, env, nil, "checkout-index", "-a", "-f"); err != nil {
		return err
	}
	_, err := git(ctx, repo, env, nil, "clean", "-fdq")
	return err
}

// LastBuild is the latest build recorded in dir.
func LastBuild(dir string) (BuildResult, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, buildFile)) //nolint:gosec // our work dir
	if err != nil {
		return BuildResult{}, false
	}
	var r BuildResult
	return r, json.Unmarshal(raw, &r) == nil
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
	cut bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf, t.cut = t.buf[len(t.buf)-t.max:], true
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	s := strings.ToValidUTF8(string(t.buf), "")
	if t.cut {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = "[…]\n" + s
	}
	return s
}

func (e Env) build(ctx context.Context) error {
	if !e.Spec.Change {
		return errors.New("build is for changes to the site; this task has no repository")
	}
	if e.Spec.Build == nil || strings.TrimSpace(e.Spec.Build.Command) == "" {
		_, _ = fmt.Fprintln(e.Stdout, "This site has no build command set up, so there is nothing to build. Check your edits by reading them.")
		return nil
	}
	_, _ = fmt.Fprintf(e.Stdout, "$ %s\n", e.Spec.Build.Command)
	res, err := RunBuild(ctx, e.Dir, *e.Spec.Build)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(e.Stdout, res.Output)
	if !res.OK {
		return fmt.Errorf("the build failed (exit %d after %.0f s): fix what the output above shows and run ekin tool build again", res.ExitCode, res.Seconds)
	}
	_, _ = fmt.Fprintf(e.Stdout, "The build passed in %.0f s. Its output was cleaned up; the repository holds only your edits.\n", res.Seconds)
	return nil
}

// diffLines is how much of the patch `ekin tool diff` prints.
const diffLines = 600

func (e Env) diff(ctx context.Context) error {
	if !e.Spec.Change {
		return errors.New("diff is for changes to the site; this task has no repository")
	}
	repo := filepath.Join(e.Dir, RepoDir)
	patch, err := Diff(ctx, repo)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(e.Dir, PatchFile), patch, 0o600); err != nil {
		return err
	}
	if len(patch) == 0 {
		_, _ = fmt.Fprintln(e.Stdout, "No changes yet: the repository is as it was copied.")
		return nil
	}
	stat, err := git(ctx, repo, nil, nil, "diff", "--cached", "--stat", "--no-renames", "--no-color", "HEAD")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(e.Stdout, strings.TrimRight(string(stat), "\n"))
	lines := strings.Split(string(patch), "\n")
	if len(lines) > diffLines {
		lines = append(lines[:diffLines], fmt.Sprintf("[… %d more lines; the whole patch is in %s]", len(lines)-diffLines, PatchFile))
	}
	_, _ = fmt.Fprintln(e.Stdout, "\n"+strings.Join(lines, "\n"))
	return nil
}
