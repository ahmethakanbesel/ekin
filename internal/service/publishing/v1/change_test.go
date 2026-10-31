package publishing_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/adapter/github"
	"github.com/ahmethakanbesel/ekin/internal/adapter/youtube"
	"github.com/ahmethakanbesel/ekin/internal/agenttool"
	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	publishingdomain "github.com/ahmethakanbesel/ekin/internal/domain/publishing/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	publishingrepo "github.com/ahmethakanbesel/ekin/internal/repository/publishing/v1"
	productionsvc "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	publishingsvc "github.com/ahmethakanbesel/ekin/internal/service/publishing/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
	"github.com/ahmethakanbesel/ekin/pkg/secretbox"
)

// makeChange edits a copy of the site (about.html changed, old.html
// deleted, llms.txt added) and returns the revision's artifacts.
func makeChange(t *testing.T, repo string, base productionsvc.ChangeBase) map[string]string {
	t.Helper()
	ctx := context.Background()
	if err := agenttool.InitBase(ctx, repo); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(repo, "about.html"), []byte("<title>About us</title>\n"), 0o600)
	_ = os.WriteFile(filepath.Join(repo, "llms.txt"), []byte("# Site\n"), 0o600)
	_ = os.Remove(filepath.Join(repo, "old.html"))
	changes, err := agenttool.Changes(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	patch, _ := agenttool.Diff(ctx, repo)
	var files bytes.Buffer
	if err := agenttool.Pack(repo, changes, &files); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(productionsvc.ChangeManifest{Base: base, Files: changes})
	return map[string]string{storydomain.ArtifactPatch: string(patch), storydomain.ArtifactChange: string(manifest),
		storydomain.ArtifactChangeFiles: files.String(), storydomain.ArtifactSummary: "Added llms.txt."}
}

func changeRequest(t *testing.T, ws [16]byte, files map[string]string) productionsvc.PublishRequest {
	t.Helper()
	req := request(t, pipelinedomain.KindArticle, files, nil)
	idea, _ := storydomain.NewIdea("Add an llms.txt", "", nil)
	st, _ := storydomain.New(id.New(), storydomain.Plan{WorkspaceID: ws, Origin: storydomain.OriginAdHoc, Language: pipelinedomain.English,
		Idea: idea, Outputs: pipelinedomain.NewChangeOutputs(),
		Brief: storydomain.Brief{Findings: []storydomain.Finding{{ID: id.New(), Rule: "llms_txt_missing", URL: "https://example.com/"}}}}, id.New, time.Now())
	req.Story, req.Deliverable, req.WorkspaceID = st, st.Lead(), ws
	req.Revision = storydomain.Revision{Number: 1}
	return req
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestChangeToLocalCheckout(t *testing.T) {
	ctx := context.Background()
	checkout := t.TempDir()
	for name, body := range map[string]string{"about.html": "<title></title>\n", "old.html": "old\n", ".gitignore": "dist/\n", "dist/x.html": "built"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(checkout, name)), 0o750)
		_ = os.WriteFile(filepath.Join(checkout, name), []byte(body), 0o600)
	}
	gitRun(t, checkout, "init", "-q")
	gitRun(t, checkout, "add", "-A")
	gitRun(t, checkout, "commit", "-qm", "init")
	svc := newService(t, nil)
	ws := id.New()
	if _, err := svc.SaveBlog(ctx, ws, publishingsvc.BlogInput{BlogTarget: publishingdomain.BlogTarget{
		Kind: publishingdomain.BlogDir, Dir: checkout, ContentDir: "content"}}); err != nil {
		t.Fatal(err)
	}
	copyDir := t.TempDir()
	base, err := svc.Repositories().Snapshot(ctx, ws, copyDir)
	if err != nil || base.Source != "dir" || base.Dir != checkout {
		t.Fatalf("snapshot = %+v %v", base, err)
	}
	if _, err := os.Stat(filepath.Join(copyDir, "dist", "x.html")); !os.IsNotExist(err) {
		t.Fatal("an ignored file was copied")
	}
	if _, err := os.Stat(filepath.Join(copyDir, ".git")); !os.IsNotExist(err) {
		t.Fatal("the checkout's .git was copied")
	}
	files := makeChange(t, copyDir, base)
	pub, err := svc.Change().Publish(ctx, changeRequest(t, ws, files))
	if err != nil || pub.URL != "file://"+checkout {
		t.Fatalf("publish = %+v %v", pub, err)
	}
	if b, _ := os.ReadFile(filepath.Join(checkout, "llms.txt")); string(b) != "# Site\n" {
		t.Fatalf("llms.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(checkout, "old.html")); !os.IsNotExist(err) {
		t.Fatal("old.html was not deleted")
	}
	if _, err := svc.Change().Publish(ctx, changeRequest(t, ws, files)); err == nil {
		t.Fatal("the same patch applied twice")
	}
}

// fakeRepoHost answers the GitHub calls a change makes and records the
// trees written and pull requests opened.
type fakeRepoHost struct {
	mu      sync.Mutex
	head    string
	moved   []string // files compare reports between base and head
	trees   []map[string]any
	parents []string
	pulls   []map[string]any
	tarball []byte
}

func (f *fakeRepoHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	p := strings.TrimPrefix(r.URL.Path, "/repos/o/site")
	switch {
	case p == "/git/ref/heads/main":
		_, _ = w.Write([]byte(`{"object":{"sha":"` + f.head + `"}}`))
	case strings.HasPrefix(p, "/tarball/"):
		_, _ = w.Write(f.tarball)
	case strings.HasPrefix(p, "/compare/"):
		var files []map[string]string
		for _, m := range f.moved {
			files = append(files, map[string]string{"filename": m})
		}
		b, _ := json.Marshal(map[string]any{"files": files})
		_, _ = w.Write(b)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/git/commits/"):
		f.parents = append(f.parents, strings.TrimPrefix(p, "/git/commits/"))
		_, _ = w.Write([]byte(`{"tree":{"sha":"t"}}`))
	case p == "/git/blobs":
		_, _ = w.Write([]byte(`{"sha":"blob"}`))
	case p == "/git/trees":
		var tree map[string]any
		_ = json.Unmarshal(raw, &tree)
		f.trees = append(f.trees, tree)
		_, _ = w.Write([]byte(`{"sha":"tree2"}`))
	case p == "/git/commits":
		_, _ = w.Write([]byte(`{"sha":"commit2"}`))
	case strings.HasPrefix(p, "/git/refs"):
		_, _ = w.Write([]byte(`{}`))
	case r.Method == http.MethodGet && p == "/pulls":
		_, _ = w.Write([]byte(`[]`))
	case r.Method == http.MethodPost && p == "/pulls":
		var pr map[string]any
		_ = json.Unmarshal(raw, &pr)
		f.pulls = append(f.pulls, pr)
		_, _ = w.Write([]byte(`{"number":12,"html_url":"https://github.com/o/site/pull/12"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}
}

func siteTarball(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(h *tar.Header, body string) {
		h.Size = int64(len(body))
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	add(&tar.Header{Name: "o-site-abc/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	add(&tar.Header{Name: "o-site-abc/about.html", Typeflag: tar.TypeReg, Mode: 0o644}, "<title></title>\n")
	add(&tar.Header{Name: "o-site-abc/old.html", Typeflag: tar.TypeReg, Mode: 0o644}, "old\n")
	add(&tar.Header{Name: "o-site-abc/bin/run", Typeflag: tar.TypeReg, Mode: 0o755}, "#!/bin/sh\n")
	add(&tar.Header{Name: "o-site-abc/escape", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"}, "")
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestChangeOnGitHub(t *testing.T) {
	ctx := context.Background()
	host := &fakeRepoHost{head: "abc", tarball: siteTarball(t)}
	srv := httptest.NewServer(host)
	defer srv.Close()
	box, _ := secretbox.New([]byte("test-secret-0123456789"))
	svc := publishingsvc.NewService(publishingrepo.NewInMemory(), box, func(context.Context) *youtube.Client { return &youtube.Client{} },
		&github.Client{BaseURL: srv.URL}, slog.New(slog.DiscardHandler))
	ws := id.New()
	token := "ghtok"
	save := func(mode publishingdomain.PublishMode) {
		if _, err := svc.SaveBlog(ctx, ws, publishingsvc.BlogInput{Token: &token, BlogTarget: publishingdomain.BlogTarget{
			Kind: publishingdomain.BlogGitHub, Repo: "o/site", Mode: mode, ContentDir: "content"}}); err != nil {
			t.Fatal(err)
		}
	}
	save(publishingdomain.PublishPullRequest)
	copyDir := t.TempDir()
	base, err := svc.Repositories().Snapshot(ctx, ws, copyDir)
	if err != nil || base != (productionsvc.ChangeBase{Source: "github", Repo: "o/site", Branch: "main", SHA: "abc"}) {
		t.Fatalf("snapshot = %+v %v", base, err)
	}
	if st, err := os.Stat(filepath.Join(copyDir, "bin", "run")); err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("bin/run lost its mode: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(copyDir, "escape")); !os.IsNotExist(err) {
		t.Fatal("a link out of the repository was kept")
	}
	files := makeChange(t, copyDir, base)

	// A pull request starts from the commit the change was made on.
	host.head = "def"
	pub, err := svc.Change().Publish(ctx, changeRequest(t, ws, files))
	if err != nil || pub.PullRequest == nil || pub.PullRequest.Number != 12 || pub.URL != "https://github.com/o/site/pull/12" ||
		!strings.HasPrefix(pub.PullRequest.Branch, "ekin/change-add-an-llms-txt-") {
		t.Fatalf("publish = %+v %v", pub, err)
	}
	if host.parents[len(host.parents)-1] != "abc" {
		t.Fatalf("the pull request's commit sits on %v", host.parents)
	}
	entries := host.trees[len(host.trees)-1]["tree"].([]any)
	got := map[string]any{}
	for _, e := range entries {
		m := e.(map[string]any)
		got[m["path"].(string)] = m["sha"]
	}
	if len(got) != 3 || got["old.html"] != nil || got["llms.txt"] != "blob" || got["about.html"] != "blob" {
		t.Fatalf("tree = %v", got)
	}
	body := host.pulls[0]["body"].(string)
	if !strings.Contains(body, "Added llms.txt.") || !strings.Contains(body, "`llms_txt_missing` on https://example.com/") {
		t.Fatalf("pull request body = %q", body)
	}
	if _, ok := svc.Change().(productionsvc.MergeWatcher); !ok {
		t.Fatal("the change publisher does not read pull requests")
	}

	// A commit onto the branch is refused when someone changed the same
	// files since, and goes through otherwise.
	save(publishingdomain.PublishCommit)
	host.moved = []string{"about.html", "README.md"}
	if _, err := svc.Change().Publish(ctx, changeRequest(t, ws, files)); err == nil || !strings.Contains(err.Error(), "about.html") {
		t.Fatalf("a clashing commit: %v", err)
	}
	host.moved = []string{"README.md"}
	pub, err = svc.Change().Publish(ctx, changeRequest(t, ws, files))
	if err != nil || pub.URL != "https://github.com/o/site/commit/commit2" || pub.PullRequest != nil {
		t.Fatalf("commit = %+v %v", pub, err)
	}
}
