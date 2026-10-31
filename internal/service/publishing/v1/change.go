package publishing

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
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/adapter/github"
	"github.com/ahmethakanbesel/ekin/internal/agenttool"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	publishingdomain "github.com/ahmethakanbesel/ekin/internal/domain/publishing/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	productionsvc "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	"github.com/ahmethakanbesel/ekin/pkg/fold"
)

// Repository copy limits.
const (
	maxRepoBytes = 1 << 30 // uncompressed
	maxRepoFiles = 100_000
	maxChangeIn  = 64 << 20
)

// Repositories copies the site's repository for changes.
func (s *Service) Repositories() productionsvc.Repositories { return repositories{s} }

// Change returns the publisher of changes to the site's repository.
func (s *Service) Change() productionsvc.Publisher { return changePublisher{s} }

type repositories struct{ s *Service }

func (r repositories) target(ctx context.Context, ws uuid.UUID) (publishingdomain.BlogTarget, error) {
	b, ok, err := r.s.Blog(ctx, ws)
	if err != nil {
		return b, err
	}
	if !ok {
		return b, apperror.New(apperror.ValidationFailed, "connect the site's repository in Settings › Publishing › Blog first")
	}
	return b, nil
}

func (r repositories) Snapshot(ctx context.Context, ws uuid.UUID, dest string) (productionsvc.ChangeBase, error) {
	b, err := r.target(ctx, ws)
	if err != nil {
		return productionsvc.ChangeBase{}, err
	}
	switch b.Kind {
	case publishingdomain.BlogGitHub:
		token, err := r.s.box.Open(b.SealedToken)
		if err != nil {
			return productionsvc.ChangeBase{}, err
		}
		sha, err := r.s.gh.Head(ctx, token, b.Repo, b.Branch)
		if err != nil {
			return productionsvc.ChangeBase{}, err
		}
		base := productionsvc.ChangeBase{Source: "github", Repo: b.Repo, Branch: b.Branch, SHA: sha}
		return base, r.fetch(ctx, token, base, dest)
	case publishingdomain.BlogDir:
		base := productionsvc.ChangeBase{Source: "dir", Dir: b.Dir}
		return base, copyCheckout(ctx, b.Dir, dest)
	}
	return productionsvc.ChangeBase{}, errors.New("unknown blog target kind")
}

func (r repositories) SnapshotAt(ctx context.Context, ws uuid.UUID, base productionsvc.ChangeBase, dest string) error {
	switch base.Source {
	case "github":
		b, err := r.target(ctx, ws)
		if err != nil {
			return err
		}
		if b.Kind != publishingdomain.BlogGitHub || b.Repo != base.Repo {
			return errors.New("the site's repository changed since this change was started; reject it and start a new one")
		}
		token, err := r.s.box.Open(b.SealedToken)
		if err != nil {
			return err
		}
		return r.fetch(ctx, token, base, dest)
	case "dir":
		return copyCheckout(ctx, base.Dir, dest)
	}
	return fmt.Errorf("unknown repository source %q", base.Source)
}

func (r repositories) fetch(ctx context.Context, token string, base productionsvc.ChangeBase, dest string) error {
	rc, err := r.s.gh.Tarball(ctx, token, base.Repo, base.SHA)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return untarRepo(rc, dest)
}

// untarRepo extracts a GitHub tarball into dest, dropping its top-level
// directory. Entries that would leave dest, and links pointing out of
// it, are refused.
func untarRepo(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("repository archive: %w", err)
	}
	tr := tar.NewReader(gz)
	var total int64
	files := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("repository archive: %w", err)
		}
		_, rel, ok := strings.Cut(path.Clean(h.Name), "/")
		if !ok || rel == "" || h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		rel = path.Clean(rel)
		if strings.HasPrefix(rel, "../") || path.IsAbs(rel) || rel == ".git" || strings.HasPrefix(rel, ".git/") {
			return fmt.Errorf("repository archive: unsafe path %q", h.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if files++; files > maxRepoFiles {
				return errors.New("the repository has too many files to copy")
			}
			if total += h.Size; total > maxRepoBytes {
				return errors.New("the repository is too large to copy")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // checked above
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, h.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			link := path.Clean(path.Join(path.Dir(rel), h.Linkname))
			if path.IsAbs(h.Linkname) || strings.HasPrefix(link, "../") || link == ".." {
				continue // a link out of the repository is left out
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			_ = os.Symlink(h.Linkname, target)
		}
	}
}

// copyCheckout copies a local checkout's files as git sees them (tracked
// and untracked, without ignored ones) into dest.
func copyCheckout(ctx context.Context, dir, dest string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "-co", "--exclude-standard", "-z")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("the checkout %s must be a git repository: %w", dir, err)
	}
	var total int64
	for i, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		if i > maxRepoFiles {
			return errors.New("the checkout has too many files to copy")
		}
		src := filepath.Join(dir, filepath.FromSlash(rel))
		st, err := os.Lstat(src)
		if err != nil || !st.Mode().IsRegular() {
			continue // deleted meanwhile, or a link
		}
		if total += st.Size(); total > maxRepoBytes {
			return errors.New("the checkout is too large to copy")
		}
		b, err := os.ReadFile(src) //nolint:gosec // the configured checkout
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(target, b, st.Mode().Perm()|0o600); err != nil {
			return err
		}
	}
	return nil
}

type changePublisher struct{ s *Service }

func (p changePublisher) Kind() pipelinedomain.Kind { return pipelinedomain.KindChange }

func (p changePublisher) Ready(ctx context.Context, ws uuid.UUID) (bool, error) {
	_, ok, err := p.s.Blog(ctx, ws)
	return ok, err
}

func (p changePublisher) Publish(ctx context.Context, req productionsvc.PublishRequest) (storydomain.Publication, error) {
	s := p.s
	b, err := s.repo.Blog(ctx, req.WorkspaceID)
	if err != nil {
		return storydomain.Publication{}, err
	}
	raw, err := readAll(ctx, req, storydomain.ArtifactChange)
	if err != nil {
		return storydomain.Publication{}, err
	}
	var m productionsvc.ChangeManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return storydomain.Publication{}, fmt.Errorf("change manifest: %w", err)
	}
	if len(m.Files) == 0 {
		return storydomain.Publication{}, errors.New("the change has no files")
	}
	title := "site: " + req.Story.Idea().Title
	summary, _ := readAll(ctx, req, storydomain.ArtifactSummary)

	if b.Kind == publishingdomain.BlogDir {
		patch, err := readAll(ctx, req, storydomain.ArtifactPatch)
		if err != nil {
			return storydomain.Publication{}, err
		}
		if err := agenttool.ApplyPatch(ctx, b.Dir, patch); err != nil {
			return storydomain.Publication{}, fmt.Errorf("the checkout changed where this change edits it: %w", err)
		}
		return storydomain.Publication{URL: "file://" + b.Dir, ExternalID: "patch"}, nil
	}
	if b.Kind != publishingdomain.BlogGitHub {
		return storydomain.Publication{}, errors.New("unknown blog target kind")
	}
	rc, _, err := req.Open(ctx, storydomain.ArtifactChangeFiles)
	if err != nil {
		return storydomain.Publication{}, err
	}
	contents, executable, err := agenttool.Unpack(rc, maxChangeIn)
	_ = rc.Close()
	if err != nil {
		return storydomain.Publication{}, err
	}
	files := make([]github.File, 0, len(m.Files))
	paths := make([]string, 0, len(m.Files))
	for _, f := range m.Files {
		paths = append(paths, f.Path)
		if f.Status == "deleted" {
			files = append(files, github.File{Path: f.Path, Delete: true})
			continue
		}
		c, ok := contents[f.Path]
		if !ok {
			return storydomain.Publication{}, fmt.Errorf("the change is missing the contents of %s", f.Path)
		}
		files = append(files, github.File{Path: f.Path, Content: c, Executable: executable[f.Path]})
	}
	token, err := s.box.Open(b.SealedToken)
	if err != nil {
		return storydomain.Publication{}, err
	}
	sameBase := m.Base.Source == "github" && m.Base.Repo == b.Repo && m.Base.Branch == b.Branch && m.Base.SHA != ""
	if b.PullRequests() {
		in := github.PullRequestInput{Repo: b.Repo, Base: b.Branch, Branch: branchFor("change-"+slug(req.Story.Idea().Title), req.Deliverable),
			Title: title, Body: changeBody(string(summary), req), Files: files}
		if sameBase {
			in.From = m.Base.SHA
		}
		pr, err := s.gh.OpenPullRequest(ctx, token, in)
		if err != nil {
			return storydomain.Publication{}, err
		}
		return storydomain.Publication{URL: pr.URL, ExternalID: pr.Branch, PullRequest: &storydomain.PullRequest{
			Repo: b.Repo, Number: pr.Number, URL: pr.URL, Branch: pr.Branch,
		}}, nil
	}
	if sameBase {
		// Committing onto the branch's tip writes whole files: refuse
		// when someone changed one of them since the change was made.
		head, err := s.gh.Head(ctx, token, b.Repo, b.Branch)
		if err != nil {
			return storydomain.Publication{}, err
		}
		moved, err := s.gh.ChangedFiles(ctx, token, b.Repo, m.Base.SHA, head)
		if err != nil {
			return storydomain.Publication{}, err
		}
		var clash []string
		for _, f := range moved {
			if slices.Contains(paths, f) {
				clash = append(clash, f)
			}
		}
		if len(clash) > 0 {
			return storydomain.Publication{}, fmt.Errorf("%s changed on %s since this change was made (%s); reject it and start it again",
				b.Repo, b.Branch, strings.Join(clash, ", "))
		}
	}
	sha, err := s.gh.Commit(ctx, token, b.Repo, b.Branch, title+"\n\n"+strings.TrimSpace(string(summary)), files)
	if err != nil {
		return storydomain.Publication{}, err
	}
	return storydomain.Publication{URL: "https://github.com/" + b.Repo + "/commit/" + sha, ExternalID: sha}, nil
}

func (p changePublisher) PullRequestState(ctx context.Context, ws uuid.UUID, pr storydomain.PullRequest) (productionsvc.PullState, error) {
	return articlePublisher(p).PullRequestState(ctx, ws, pr)
}

func changeBody(summary string, req productionsvc.PublishRequest) string {
	var b strings.Builder
	if s := strings.TrimSpace(summary); s != "" {
		b.WriteString(s + "\n\n")
	}
	if fs := req.Story.Brief().Findings; len(fs) > 0 {
		b.WriteString("Site audit findings this sets out to fix:\n\n")
		for _, f := range fs {
			fmt.Fprintf(&b, "- `%s` on %s\n", f.Rule, f.URL)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Approved in Ekin (revision %d). The next crawl after the merge checks the findings again.\n", req.Revision.Number)
	return b.String()
}

// slug makes a branch-safe name from a title.
func slug(title string) string {
	var b bytes.Buffer
	dash := false
	for _, r := range fold.String(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}
