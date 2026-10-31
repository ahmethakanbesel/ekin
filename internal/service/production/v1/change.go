package production

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/agenttool"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	sitesvc "github.com/ahmethakanbesel/ekin/internal/service/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// KindChange drafts a change to the site's repository. It is a kind of
// its own, so only workers that share the server's database (and its
// repository credentials) take it, never a remote hybrid worker.
const KindChange = "change.draft"

// FindingsPerChange is how many findings of one rule a pipeline's change
// takes on.
const FindingsPerChange = 10

// ChangeBase is where a change's copy of the repository came from.
type ChangeBase struct {
	// Source is "github" or "dir" (a local checkout).
	Source string `json:"source"`
	Repo   string `json:"repo,omitempty"`   // owner/name (github)
	Branch string `json:"branch,omitempty"` // the branch it was copied from
	SHA    string `json:"sha,omitempty"`    // the commit it was copied at
	Dir    string `json:"dir,omitempty"`    // the checkout (dir)
}

// Repositories copies the site's repository for a change (the
// publishing service, which holds the repository's settings).
type Repositories interface {
	// Snapshot writes the repository at the tip of its branch into dest.
	Snapshot(ctx context.Context, workspaceID uuid.UUID, dest string) (ChangeBase, error)
	// SnapshotAt writes base's commit into dest again.
	SnapshotAt(ctx context.Context, workspaceID uuid.UUID, base ChangeBase, dest string) error
}

// Sites is what changes need from the site service: its build command,
// the findings to fix and the pages they are on.
type Sites interface {
	ChangeSite(ctx context.Context, workspaceID uuid.UUID) (sitesvc.ChangeSite, error)
	Claim(ctx context.Context, workspaceID, storyID uuid.UUID, ids []uuid.UUID) ([]*sitedomain.Finding, error)
	Release(ctx context.Context, workspaceID, storyID uuid.UUID) (int, error)
	NextFindings(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*sitedomain.Finding, error)
	PagesAt(ctx context.Context, workspaceID uuid.UUID, urls []string) ([]sitedomain.Page, error)
}

// ChangeManifest is a change revision's change.json: where its copy
// came from, the files it touches and its last build.
type ChangeManifest struct {
	Base  ChangeBase              `json:"base"`
	Files []agenttool.ChangedFile `json:"files"`
	Build *agenttool.BuildResult  `json:"build,omitempty"`
}

const baseFile = ".ekin/base.json"

// ChangeRequest asks for one change: findings to fix, instructions, or
// both.
type ChangeRequest struct {
	FindingIDs   []uuid.UUID
	Instructions string
	Language     pipelinedomain.Language
	Production   pipelinedomain.Production
}

// MaxChangeInstructions bounds a change request's instructions.
const MaxChangeInstructions = 5000

// QueueChange starts a change story at once (no ideas to plan): it
// claims the findings, and the change is queued for the agent.
func (s *Service) QueueChange(ctx context.Context, workspaceID, actorID uuid.UUID, req ChangeRequest) (*storydomain.Story, error) {
	if s.sites == nil || s.repos == nil {
		return nil, apperror.New(apperror.ValidationFailed, "changes to the site are not available on this server")
	}
	req.Instructions = strings.TrimSpace(req.Instructions)
	if len(req.FindingIDs) == 0 && req.Instructions == "" {
		return nil, apperror.New(apperror.ValidationFailed, "choose findings to fix or describe the change")
	}
	if utf8.RuneCountInString(req.Instructions) > MaxChangeInstructions {
		return nil, apperror.New(apperror.ValidationFailed, "the instructions are too long")
	}
	if len(req.FindingIDs) > storydomain.MaxFindings {
		return nil, apperror.New(apperror.ValidationFailed, fmt.Sprintf("a change takes on at most %d findings", storydomain.MaxFindings))
	}
	if req.Language == "" {
		req.Language = pipelinedomain.English
	}
	outputs := pipelinedomain.NewChangeOutputs()
	if err := req.Production.CheckFor(outputs); err != nil {
		return nil, err
	}
	if _, err := s.sites.ChangeSite(ctx, workspaceID); err != nil {
		if apperror.Is(err, apperror.NotFound) {
			return nil, apperror.New(apperror.ValidationFailed, "set up the site in Settings › Site first")
		}
		return nil, err
	}
	prod, _, err := s.resolve(ctx, workspaceID, req.Production, pipelinedomain.TemplateChoice{None: true}, outputs)
	if err != nil {
		return nil, err
	}
	storyID := id.New()
	var st *storydomain.Story
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var findings []*sitedomain.Finding
		if len(req.FindingIDs) > 0 {
			var err error
			if findings, err = s.sites.Claim(ctx, workspaceID, storyID, req.FindingIDs); err != nil {
				return err
			}
		}
		idea, err := changeIdea(findings, req.Instructions)
		if err != nil {
			return err
		}
		st, err = storydomain.New(storyID, storydomain.Plan{
			WorkspaceID: workspaceID, TraceID: jobdomain.TraceFrom(ctx), Origin: storydomain.OriginAdHoc, Language: req.Language,
			Idea: idea, Outputs: outputs, CreatedBy: actorID,
			Brief: storydomain.Brief{Instructions: req.Instructions, Production: prod, Findings: briefFindings(findings)},
		}, id.New, s.now())
		if err != nil {
			return err
		}
		if err := s.stories.Create(ctx, st); err != nil {
			return err
		}
		return s.enqueueStep(ctx, st, st.Lead())
	})
	return st, err
}

// planFindings runs a findings pipeline: each of count changes takes on
// the open findings of the most severe rule left.
func (s *Service) planFindings(ctx context.Context, p *pipelinedomain.Pipeline, origin storydomain.Origin, count int) error {
	if s.sites == nil || s.repos == nil {
		return jobdomain.Permanent(errors.New("changes to the site are not available on this server"))
	}
	if _, err := s.sites.ChangeSite(ctx, p.WorkspaceID()); err != nil {
		if apperror.Is(err, apperror.NotFound) {
			return jobdomain.Permanent(errors.New("the site is not set up: set it up in Settings › Site"))
		}
		return err
	}
	prod, _, err := s.resolve(ctx, p.WorkspaceID(), p.Production(), pipelinedomain.TemplateChoice{None: true}, p.Outputs())
	if err != nil {
		return err
	}
	created := 0
	for range max(count, 1) {
		next, err := s.sites.NextFindings(ctx, p.WorkspaceID(), FindingsPerChange)
		if err != nil {
			return err
		}
		if len(next) == 0 {
			break
		}
		ids := make([]uuid.UUID, 0, len(next))
		for _, f := range next {
			ids = append(ids, f.ID())
		}
		storyID := id.New()
		err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
			findings, err := s.sites.Claim(ctx, p.WorkspaceID(), storyID, ids)
			if err != nil {
				return err
			}
			idea, err := changeIdea(findings, "")
			if err != nil {
				return err
			}
			st, err := storydomain.New(storyID, storydomain.Plan{
				WorkspaceID: p.WorkspaceID(), PipelineID: p.ID(), TraceID: jobdomain.TraceFrom(ctx), Origin: origin,
				Language: p.Language(), Idea: idea, Outputs: p.Outputs(),
				Brief: storydomain.Brief{Instructions: p.Source().Instructions(), Production: prod, Findings: briefFindings(findings)},
			}, id.New, s.now())
			if err != nil {
				return err
			}
			if err := s.stories.Create(ctx, st); err != nil {
				return err
			}
			return s.enqueueStep(ctx, st, st.Lead())
		})
		if err != nil {
			return err
		}
		created++
	}
	if created == 0 {
		return jobdomain.Permanent(errors.New("no open findings to fix: every one is fixed, ignored or taken by a change"))
	}
	s.notify(ctx, Event{WorkspaceID: p.WorkspaceID(), Kind: "run", Title: p.Name(),
		Detail: fmt.Sprintf("%d new changes in production", created)})
	return nil
}

// changeIdea names a change after what it fixes, or after its
// instructions.
func changeIdea(findings []*sitedomain.Finding, instructions string) (storydomain.Idea, error) {
	if len(findings) == 0 {
		title, _, _ := strings.Cut(instructions, "\n")
		title = strings.TrimSpace(title)
		if r := []rune(title); len(r) > 100 {
			title = strings.TrimSpace(string(r[:100])) + "…"
		}
		return storydomain.NewIdea(title, instructions, nil)
	}
	rules := map[sitedomain.Rule]int{}
	var order []sitedomain.Rule
	for _, f := range findings {
		if rules[f.Rule()] == 0 {
			order = append(order, f.Rule())
		}
		rules[f.Rule()]++
	}
	title := order[0].Fix()
	if len(order) > 1 {
		title = fmt.Sprintf("Fix %d kinds of site findings", len(order))
	} else if n := rules[order[0]]; n > 1 {
		title += fmt.Sprintf(" (%d pages)", n)
	}
	var angle strings.Builder
	for _, r := range order {
		fmt.Fprintf(&angle, "%s: %d finding(s). ", r.Fix(), rules[r])
	}
	if instructions != "" {
		angle.WriteString(instructions)
	}
	return storydomain.NewIdea(title, strings.TrimSpace(angle.String()), nil)
}

func briefFindings(fs []*sitedomain.Finding) []storydomain.Finding {
	out := make([]storydomain.Finding, 0, len(fs))
	for _, f := range fs {
		out = append(out, storydomain.Finding{ID: f.ID(), Rule: string(f.Rule()), Severity: string(f.Severity()), URL: f.URL(),
			Evidence: strings.Join(f.Evidence(), "\n")})
	}
	return out
}

// releaseFindings gives back the findings a dropped change claimed.
func (s *Service) releaseFindings(ctx context.Context, st *storydomain.Story) {
	if s.sites == nil || len(st.Brief().Findings) == 0 {
		return
	}
	if _, err := s.sites.Release(ctx, st.WorkspaceID(), st.ID()); err != nil {
		s.log.WarnContext(ctx, "cannot release a dropped change's findings", slog.String("story", st.ID().String()), slog.Any("error", err))
	}
}

// prepareChange makes dir hold the repository with the change so far,
// and writes the instructions and brief.
func (s *Service) prepareChange(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, dir string) (task, error) {
	t := task{spec: agenttool.Spec{Language: string(d.Language()), Change: true}}
	if s.sites == nil || s.repos == nil {
		return t, errors.New("changes to the site are not available on this server")
	}
	site, err := s.sites.ChangeSite(ctx, st.WorkspaceID())
	if err != nil {
		if apperror.Is(err, apperror.NotFound) {
			return t, errors.New("the site is no longer set up")
		}
		return t, err
	}
	if site.Build != "" {
		t.spec.Build = &agenttool.BuildSpec{Command: site.Build}
	}
	base, err := s.ensureRepo(ctx, st, dir)
	if err != nil {
		return t, err
	}
	instructions, err := execute("change.md", map[string]any{"Tool": toolCmd, "Build": site.Build, "LanguageName": languageName(d.Language())})
	if err != nil {
		return t, err
	}
	if err := os.WriteFile(filepath.Join(dir, "INSTRUCTIONS.md"), []byte(instructions), 0o600); err != nil {
		return t, err
	}
	brief, err := s.changeBrief(ctx, st, site, base)
	if err != nil {
		return t, err
	}
	return t, os.WriteFile(filepath.Join(dir, "BRIEF.md"), []byte(brief), 0o600)
}

// ensureRepo puts the repository in dir: kept from the last draft on
// this host, rebuilt from its base commit and the last revision's patch
// after a restore, or copied fresh for a first draft.
func (s *Service) ensureRepo(ctx context.Context, st *storydomain.Story, dir string) (ChangeBase, error) {
	repo := filepath.Join(dir, agenttool.RepoDir)
	var base ChangeBase
	raw, err := os.ReadFile(filepath.Join(dir, baseFile)) //nolint:gosec // our work dir
	known := err == nil && json.Unmarshal(raw, &base) == nil
	if _, err := os.Stat(filepath.Join(repo, ".git")); err == nil && known {
		return base, nil
	}
	_ = os.RemoveAll(repo)
	if err := os.MkdirAll(repo, 0o750); err != nil {
		return base, err
	}
	if known {
		if err := s.repos.SnapshotAt(ctx, st.WorkspaceID(), base, repo); err != nil {
			return base, fmt.Errorf("copy the repository again: %w", err)
		}
	} else {
		if base, err = s.repos.Snapshot(ctx, st.WorkspaceID(), repo); err != nil {
			return base, fmt.Errorf("copy the repository: %w", err)
		}
		raw, _ := json.MarshalIndent(base, "", "  ")
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, baseFile)), 0o750); err != nil {
			return base, err
		}
		if err := os.WriteFile(filepath.Join(dir, baseFile), raw, 0o600); err != nil {
			return base, err
		}
	}
	if err := agenttool.InitBase(ctx, repo); err != nil {
		return base, err
	}
	if known {
		patch, err := os.ReadFile(filepath.Join(dir, agenttool.PatchFile)) //nolint:gosec // our work dir
		if err == nil {
			if err := agenttool.ApplyPatch(ctx, repo, patch); err != nil {
				return base, fmt.Errorf("bring the change back onto the repository: %w", err)
			}
		}
	}
	return base, nil
}

func (s *Service) changeBrief(ctx context.Context, st *storydomain.Story, site sitesvc.ChangeSite, base ChangeBase) (string, error) {
	var b strings.Builder
	idea := st.Idea()
	fmt.Fprintf(&b, "# Brief\n\n- **Site:** %s\n", site.URL)
	switch base.Source {
	case "github":
		fmt.Fprintf(&b, "- **Repository:** %s, branch %s at %s, copied into `repo/`\n", base.Repo, base.Branch, short(base.SHA))
	default:
		b.WriteString("- **Repository:** the site's checkout, copied into `repo/`\n")
	}
	if site.Build != "" {
		fmt.Fprintf(&b, "- **Build:** `%s`, run by `%s build`\n", site.Build, toolCmd)
	} else {
		b.WriteString("- **Build:** none set up; check your edits by reading them\n")
	}
	fmt.Fprintf(&b, "\n## The change\n\n**%s**\n", idea.Title)
	if ins := strings.TrimSpace(st.Brief().Instructions); ins != "" {
		fmt.Fprintf(&b, "\n## Instructions\n\n%s\n", ins)
	}
	findings := st.Brief().Findings
	if len(findings) > 0 {
		b.WriteString("\n## Findings to fix\n\nThe site audit raised these on the live site. Fix each at its source in the repository; " +
			"the next crawl checks whether they are gone.\n\n")
		urls := make([]string, 0, len(findings))
		for i, f := range findings {
			fmt.Fprintf(&b, "%d. **%s** (%s) on %s\n", i+1, sitedomain.Rule(f.Rule).Fix(), f.Severity, f.URL)
			for line := range strings.SplitSeq(strings.TrimSpace(f.Evidence), "\n") {
				if line != "" {
					fmt.Fprintf(&b, "   - %s\n", line)
				}
			}
			urls = append(urls, f.URL)
		}
		pages, err := s.sites.PagesAt(ctx, st.WorkspaceID(), urls)
		if err != nil {
			return "", err
		}
		if len(pages) > 0 {
			b.WriteString("\n## The pages as the last crawl saw them\n\nUse these to find each page's source file.\n\n")
			for _, p := range pages {
				fmt.Fprintf(&b, "- %s: HTTP %d", p.URL, p.Status)
				if p.Title != "" {
					fmt.Fprintf(&b, ", title %q", p.Title)
				}
				if len(p.H1) > 0 {
					fmt.Fprintf(&b, ", h1 %q", p.H1[0])
				}
				if p.Description != "" {
					fmt.Fprintf(&b, ", description %q", p.Description)
				}
				if p.Canonical != "" {
					fmt.Fprintf(&b, ", canonical %s", p.Canonical)
				}
				if p.RedirectTo != "" {
					fmt.Fprintf(&b, ", redirects to %s", p.RedirectTo)
				}
				b.WriteString("\n")
			}
		}
	}
	return b.String(), nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// changeAllowed are a change agent's shell commands: the build and the
// diff, as the instructions spell them and by absolute path.
func (s *Service) changeAllowed(ctx context.Context) []string {
	var allowed []string
	bins := []string{"ekin"}
	if dir := s.binDir(ctx); dir != "" {
		bins = append(bins, filepath.Join(dir, "ekin"))
	}
	if bin := s.cfg.EkinBin; bin != "" && !strings.ContainsAny(bin, " ()*") {
		bins = append(bins, bin)
	}
	for _, b := range bins {
		allowed = append(allowed, "Bash("+b+" tool build)", "Bash("+b+" tool diff)")
	}
	return allowed
}

// validateChange checks a change the way review needs it: a summary, a
// non-empty patch within limits that leaves the CI workflows alone, and
// a build that passes on exactly these edits (run here when the agent's
// last build was on other edits).
func (s *Service) validateChange(ctx context.Context, t DraftTask, dir string) []string {
	var problems []string
	if b, err := os.ReadFile(filepath.Join(dir, agenttool.SummaryFile)); err != nil || strings.TrimSpace(string(b)) == "" { //nolint:gosec // our work dir
		problems = append(problems, agenttool.SummaryFile+" is missing or empty: say what you changed and which findings it fixes")
	}
	repo := filepath.Join(dir, agenttool.RepoDir)
	changes, err := agenttool.Changes(ctx, repo)
	if err != nil {
		return append(problems, err.Error())
	}
	switch {
	case len(changes) == 0:
		return append(problems, "the repository has no changes: make the edits in repo/")
	case len(changes) > agenttool.MaxChangedFiles:
		problems = append(problems, fmt.Sprintf("the change touches %d files; keep it to at most %d", len(changes), agenttool.MaxChangedFiles))
	}
	for _, c := range changes {
		if strings.HasPrefix(c.Path, ".github/") {
			problems = append(problems, c.Path+": leave .github/ (CI workflows and repository settings) alone")
		}
	}
	patch, err := agenttool.Diff(ctx, repo)
	if err != nil {
		return append(problems, err.Error())
	}
	if len(patch) > agenttool.MaxPatchBytes {
		problems = append(problems, fmt.Sprintf("the patch is %d bytes; keep the change under %d", len(patch), agenttool.MaxPatchBytes))
	}
	if err := os.WriteFile(filepath.Join(dir, agenttool.PatchFile), patch, 0o600); err != nil {
		return append(problems, err.Error())
	}
	if len(problems) > 0 || t.Spec.Build == nil {
		return problems
	}
	tree, err := agenttool.Tree(ctx, repo)
	if err != nil {
		return append(problems, err.Error())
	}
	if last, ok := agenttool.LastBuild(dir); ok && last.OK && last.Tree == tree {
		return nil
	}
	res, err := agenttool.RunBuild(ctx, dir, *t.Spec.Build)
	if err != nil {
		return append(problems, "the build could not run: "+err.Error())
	}
	if !res.OK {
		problems = append(problems, fmt.Sprintf("the build fails on these edits (exit %d); run %s build, fix what it shows, and run it again until it passes:\n\n%s",
			res.ExitCode, toolCmd, lastLines(res.Output, 60)))
	}
	return problems
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// archiveChange stores a change revision's patch, manifest, files and
// summary under prefix, adding their keys to out.
func (s *Service) archiveChange(ctx context.Context, dir, prefix string, out map[string]string) error {
	repo := filepath.Join(dir, agenttool.RepoDir)
	changes, err := agenttool.Changes(ctx, repo)
	if err != nil {
		return err
	}
	patch, err := agenttool.Diff(ctx, repo)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, agenttool.PatchFile), patch, 0o600); err != nil {
		return err
	}
	m := ChangeManifest{Files: changes}
	if raw, err := os.ReadFile(filepath.Join(dir, baseFile)); err == nil { //nolint:gosec // our work dir
		_ = json.Unmarshal(raw, &m.Base)
	}
	if b, ok := agenttool.LastBuild(dir); ok {
		if tree, err := agenttool.Tree(ctx, repo); err == nil && tree == b.Tree {
			m.Build = &b
		}
	}
	manifest, _ := json.MarshalIndent(m, "", "  ")
	var files bytes.Buffer
	if err := agenttool.Pack(repo, changes, &files); err != nil {
		return err
	}
	put := func(name string, b []byte) error {
		if err := s.blobs.Put(ctx, prefix+name, bytes.NewReader(b), int64(len(b))); err != nil {
			return err
		}
		out[name] = prefix + name
		return nil
	}
	if err := put(storydomain.ArtifactPatch, patch); err != nil {
		return err
	}
	if err := put(storydomain.ArtifactChange, manifest); err != nil {
		return err
	}
	if err := put(storydomain.ArtifactChangeFiles, files.Bytes()); err != nil {
		return err
	}
	if summary, err := os.ReadFile(filepath.Join(dir, agenttool.SummaryFile)); err == nil { //nolint:gosec // our work dir
		return put(storydomain.ArtifactSummary, summary)
	}
	return nil
}
