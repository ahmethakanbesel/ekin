package production

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/google/uuid"

	"kurgu/internal/adapter/claude"
	"kurgu/internal/agenttool"
	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
)

//go:embed prompts/*.md
var promptFS embed.FS

var prompts = template.Must(template.New("").ParseFS(promptFS, "prompts/*.md"))

func execute(name string, data any) (string, error) {
	var b bytes.Buffer
	if err := prompts.ExecuteTemplate(&b, name, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// videoFrame is the frame size of a video spec.
func videoFrame(v pipelinedomain.VideoSpec) (int, int) {
	switch v.Orientation() {
	case pipelinedomain.Horizontal:
		return 1920, 1080
	case pipelinedomain.Square:
		return 1080, 1080
	}
	return 1080, 1920
}

// durationBounds allows ±25% around the target, within the format.
func durationBounds(v pipelinedomain.VideoSpec) (float64, float64) {
	target := v.Duration().Seconds()
	lo, hi := math.Max(target*0.75, 8), target*1.25
	if v.Format() == pipelinedomain.FormatShort {
		hi = math.Min(hi, pipelinedomain.MaxShortDuration.Seconds())
	}
	return math.Round(lo), math.Round(hi)
}

func (s *Service) workDir(d *storydomain.Deliverable) string {
	return filepath.Join(s.cfg.WorkRoot, d.ID().String())
}

// Draft runs a deliverable.draft job: the agent writes (or revises) the
// deliverable in its working directory, Kurgu validates and archives the
// result as the next revision.
func (s *Service) Draft(ctx context.Context, p DeliverablePayload) error {
	var feedback *storydomain.Feedback
	st, err := s.mutate(ctx, p.StoryID, func(_ context.Context, st *storydomain.Story) error {
		var err error
		feedback, err = st.StartDrafting(p.DeliverableID, s.now())
		return err
	})
	if apperror.Is(err, apperror.InvalidTransition) || apperror.Is(err, apperror.StoryNotFound) {
		return jobdomain.Permanent(err) // already handled, rejected or deleted
	}
	if err != nil {
		return err
	}
	d, _ := st.Deliverable(p.DeliverableID)
	artifacts, session, notes, err := s.runAgent(ctx, st, d, feedback)
	if err != nil {
		s.log.ErrorContext(ctx, "draft failed", slog.String("deliverable", d.ID().String()), slog.Any("error", err))
		s.fail(ctx, st, d, err)
		return nil
	}
	var rev storydomain.Revision
	st, err = s.mutate(ctx, p.StoryID, func(ctx context.Context, st *storydomain.Story) error {
		var err error
		rev, err = st.Drafted(p.DeliverableID, artifacts, session, notes, s.now())
		if err != nil {
			return err
		}
		if err := s.stories.PutRevision(ctx, rev); err != nil {
			return err
		}
		d, _ := st.Deliverable(p.DeliverableID)
		return s.enqueueStep(ctx, st, d)
	})
	if err != nil {
		return err
	}
	if d, _ := st.Deliverable(p.DeliverableID); d.Status() == storydomain.StatusReview {
		s.notify(ctx, Event{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), Kind: "review", Title: st.Idea().Title,
			Detail: fmt.Sprintf("%s revision %d is ready for review", d.Kind(), rev.Number)})
	}
	return nil
}

// fail marks the deliverable failed and tells the workspace.
func (s *Service) fail(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, cause error) {
	reason := cause.Error()
	var ce *claude.Error
	if errors.As(cause, &ce) {
		reason = "agent: " + ce.Subtype + ": " + ce.Message
	}
	_, err := s.mutate(context.WithoutCancel(ctx), st.ID(), func(_ context.Context, st *storydomain.Story) error {
		return st.Fail(d.ID(), reason, s.now())
	})
	if err != nil {
		s.log.ErrorContext(ctx, "cannot record failure", slog.Any("error", err))
		return
	}
	s.notify(ctx, Event{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), Kind: "failed", Title: st.Idea().Title,
		Detail: fmt.Sprintf("%s failed: %s", d.Kind(), firstLine(reason))})
}

// runAgent prepares the working directory, runs the agent until its
// output validates, and archives the result.
func (s *Service) runAgent(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, fb *storydomain.Feedback) (map[string]string, string, string, error) {
	dir := s.workDir(d)
	if err := s.ensureWorkDir(ctx, d, dir); err != nil {
		return nil, "", "", err
	}
	task, err := s.prepare(ctx, st, d, dir)
	if err != nil {
		return nil, "", "", err
	}
	req := claude.Request{
		Dir:     dir,
		Tools:   []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash"},
		Allowed: []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash(" + s.cfg.KurguBin + " tool:*)"},
		Timeout: s.cfg.AgentTimeout,
	}
	if !d.IsVideo() {
		req.Tools = []string{"Read", "Write", "Edit", "Glob", "Grep"}
		req.Allowed = req.Tools
	}
	if fb != nil && d.Session() != "" {
		req.Resume = d.Session()
		req.Prompt, err = execute("revise.md", map[string]any{
			"Revision": d.Revision(), "Feedback": fb.Text, "Scope": scopeName(fb.Scope), "At": atSeconds(fb), "Tool": s.toolCmd(),
		})
	} else {
		req.Prompt = "Read INSTRUCTIONS.md, then BRIEF.md, and do the task they describe."
		if fb != nil {
			req.Prompt += fmt.Sprintf("\n\nA reviewer asked for these changes to the previous version (kept in this directory): %s", fb.Text)
		}
	}
	if err != nil {
		return nil, "", "", err
	}
	req.Env = s.toolEnv(task)
	runner := s.agent
	res, err := runner.Run(ctx, req)
	if err != nil {
		return nil, "", "", err
	}
	session := res.SessionID
	for attempt := 0; ; attempt++ {
		problems := s.validate(ctx, d, dir, task)
		if len(problems) == 0 {
			break
		}
		if attempt >= s.cfg.FixAttempts {
			return nil, "", "", fmt.Errorf("output still invalid after %d fixes: %s", attempt, strings.Join(problems, "; "))
		}
		res, err = runner.Run(ctx, claude.Request{
			Dir: dir, Tools: req.Tools, Allowed: req.Allowed, Timeout: s.cfg.AgentTimeout, Resume: session, Env: req.Env,
			Prompt: "Kurgu's own check of your output failed:\n\n- " + strings.Join(problems, "\n- ") +
				"\n\nFix every item, check again, and reply when done.",
		})
		if err != nil {
			return nil, "", "", err
		}
		if res.SessionID != "" {
			session = res.SessionID
		}
	}
	artifacts, err := s.archive(ctx, st, d, dir)
	if err != nil {
		return nil, "", "", err
	}
	return artifacts, session, strings.TrimSpace(res.Text), nil
}

func (s *Service) toolCmd() string { return s.cfg.KurguBin + " tool" }

// task is what the tools and the validator need to know.
type task struct {
	spec    agenttool.Spec
	article *pipelinedomain.ArticleSpec
}

func (s *Service) toolEnv(t task) []string {
	spec, _ := json.Marshal(t.spec)
	return append(append([]string(nil), s.cfg.ToolEnv...), "KURGU_TOOL_SPEC="+string(spec))
}

// prepare writes INSTRUCTIONS.md, BRIEF.md, KIT.md and the sources.
func (s *Service) prepare(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, dir string) (task, error) {
	t := task{spec: agenttool.Spec{Language: string(d.Language())}, article: d.Article()}
	write := func(name, content string) error {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(content), 0o600)
	}
	_ = os.RemoveAll(filepath.Join(dir, "frames"))

	var sources string
	if refs := st.Brief().Refs; len(refs) > 0 {
		ex, err := s.knowledge.Excerpt(ctx, st.WorkspaceID(), toKnowledgeRefs(refs))
		if err != nil {
			return t, err
		}
		sources = ex.Markdown
		if err := write("sources/pages.md", sources); err != nil {
			return t, err
		}
	}
	source, err := s.sourceDeliverable(ctx, st, d, dir)
	if err != nil {
		return t, err
	}

	var instructions string
	data := map[string]any{
		"LanguageName": languageName(d.Language()), "HasSources": sources != "" || source.text != "",
		"Tool": s.toolCmd(),
	}
	switch {
	case d.IsVideo():
		v := *d.Video()
		w, h := videoFrame(v)
		lo, hi := durationBounds(v)
		t.spec.Width, t.spec.Height, t.spec.MinDuration, t.spec.MaxDuration = w, h, lo, hi
		data["Long"] = v.Format() == pipelinedomain.FormatLong
		data["Width"], data["Height"] = w, h
		data["MinSeconds"], data["MaxSeconds"] = int(lo), int(hi)
		data["TargetWords"] = int(v.Duration().Seconds() * 2)
		if instructions, err = execute("video.md", data); err != nil {
			return t, err
		}
		kit, err := execute("kit.md", data)
		if err != nil {
			return t, err
		}
		if err := write("KIT.md", kit); err != nil {
			return t, err
		}
	case d.Role() == storydomain.RoleTranslation:
		data["SourceLanguageName"] = languageName(st.Language())
		if instructions, err = execute("translate.md", data); err != nil {
			return t, err
		}
	default:
		data["Words"] = d.Article().Words()
		data["VideoURL"], data["VideoID"] = source.videoURL, source.videoID
		if instructions, err = execute("article.md", data); err != nil {
			return t, err
		}
	}
	if err := write("INSTRUCTIONS.md", instructions); err != nil {
		return t, err
	}
	return t, write("BRIEF.md", s.brief(st, d, t, source))
}

type sourceMaterial struct {
	text     string // the lead's script or article, for derived deliverables
	videoURL string
	videoID  string
}

// sourceDeliverable copies what a derived or translated deliverable is
// built from into the working directory.
func (s *Service) sourceDeliverable(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, dir string) (sourceMaterial, error) {
	var out sourceMaterial
	if d.SourceID() == uuid.Nil {
		return out, nil
	}
	src, err := st.Deliverable(d.SourceID())
	if err != nil || src.Revision() == 0 {
		return out, err
	}
	rev, err := s.stories.Revision(ctx, src.ID(), src.Revision())
	if err != nil {
		return out, err
	}
	read := func(name string) string {
		key, ok := rev.Artifacts[name]
		if !ok {
			return ""
		}
		b, err := s.readBlob(ctx, key)
		if err != nil {
			return ""
		}
		return string(b)
	}
	if pub := src.Publication(); pub != nil && src.IsVideo() {
		out.videoURL, out.videoID = pub.URL, pub.ExternalID
	}
	switch {
	case d.Role() == storydomain.RoleTranslation:
		out.text = read(storydomain.ArtifactArticle)
		return out, os.WriteFile(filepath.Join(dir, "source.md"), []byte(out.text), 0o600)
	case src.IsVideo():
		out.text = "# Video script\n\n" + read(storydomain.ArtifactScript) + "\n\n# Video metadata\n\n" + read(storydomain.ArtifactMetadata)
	default:
		out.text = "# Article\n\n" + read(storydomain.ArtifactArticle)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sources"), 0o750); err != nil {
		return out, err
	}
	return out, os.WriteFile(filepath.Join(dir, "sources", "lead.md"), []byte(out.text), 0o600)
}

func (s *Service) brief(st *storydomain.Story, d *storydomain.Deliverable, t task, src sourceMaterial) string {
	var b strings.Builder
	idea := st.Idea()
	fmt.Fprintf(&b, "# Brief\n\n- **Language:** %s\n", languageName(d.Language()))
	if d.IsVideo() {
		v := *d.Video()
		fmt.Fprintf(&b, "- **Video:** %s, %s, %d×%d, target %d s (allowed %.0f–%.0f s), 30 fps\n",
			v.Format(), v.Orientation(), t.spec.Width, t.spec.Height, int(v.Duration().Seconds()), t.spec.MinDuration, t.spec.MaxDuration)
	}
	if a := d.Article(); a != nil {
		fmt.Fprintf(&b, "- **Article:** about %d words\n", a.Words())
	}
	switch d.Role() {
	case storydomain.RoleDerived:
		b.WriteString("- **Derived:** build this from the approved lead in `sources/lead.md`; stay consistent with it.\n")
		if src.videoURL != "" {
			fmt.Fprintf(&b, "- **Video URL:** %s\n", src.videoURL)
		}
	case storydomain.RoleTranslation:
		b.WriteString("- **Translation:** of `source.md`.\n")
	}
	fmt.Fprintf(&b, "\n## Idea\n\n**%s**\n\n%s\n", idea.Title, idea.Angle)
	if len(idea.Keywords) > 0 {
		fmt.Fprintf(&b, "\nKeywords: %s\n", strings.Join(idea.Keywords, ", "))
	}
	if ins := strings.TrimSpace(st.Brief().Instructions); ins != "" {
		fmt.Fprintf(&b, "\n## Standing instructions\n\n%s\n", ins)
	}
	if style := strings.TrimSpace(st.Brief().Style); style != "" {
		fmt.Fprintf(&b, "\n## Style\n\n%s\n", style)
	}
	if len(st.Brief().Refs) > 0 {
		b.WriteString("\n## Sources\n\n`sources/pages.md` holds the referenced pages. Stay within what they say.\n")
	}
	return b.String()
}

// validate checks the agent's output the same way the tools do, so a
// session that ended early cannot slip through.
func (s *Service) validate(ctx context.Context, d *storydomain.Deliverable, dir string, t task) []string {
	if d.IsVideo() {
		rep := agenttool.Check(ctx, s.renderer, dir, "composition.html", t.spec)
		return rep.Errors
	}
	raw, err := os.ReadFile(filepath.Join(dir, "article.md")) //nolint:gosec // our work dir
	if err != nil {
		return []string{"article.md is missing"}
	}
	a, err := ParseArticle(raw)
	if err != nil {
		return []string{err.Error()}
	}
	if t.article != nil && d.Role() != storydomain.RoleTranslation {
		target := t.article.Words()
		if a.Words < target*6/10 || a.Words > target*16/10 {
			return []string{fmt.Sprintf("the body has %d words; the target is about %d", a.Words, target)}
		}
	}
	return nil
}

func scopeName(sc storydomain.Scope) string {
	if sc == storydomain.ScopeAll || sc == "" {
		return ""
	}
	return string(sc)
}

func atSeconds(fb *storydomain.Feedback) string {
	if fb.AtSeconds == nil {
		return ""
	}
	return fmt.Sprintf("%.1f", *fb.AtSeconds)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// ensureWorkDir restores the working directory from the latest revision's
// archive when this host does not have it (another worker drafted it).
func (s *Service) ensureWorkDir(ctx context.Context, d *storydomain.Deliverable, dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if d.Revision() == 0 {
		return nil
	}
	rev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
	if err != nil {
		return err
	}
	key, ok := rev.Artifacts[storydomain.ArtifactWorkdir]
	if !ok {
		return nil
	}
	rc, _, err := s.blobs.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("restore work dir: %w", err)
	}
	defer func() { _ = rc.Close() }()
	return untar(rc, dir)
}

// archive uploads the working directory and the key outputs of this
// revision and returns their blob keys.
func (s *Service) archive(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, dir string) (map[string]string, error) {
	prefix := fmt.Sprintf("ws/%s/stories/%s/%s/r%d/", st.WorkspaceID(), st.ID(), d.ID(), d.Revision()+1)
	out := map[string]string{}
	tmp, err := os.CreateTemp("", "kurgu-workdir-*.tar.gz")
	if err != nil {
		return nil, err
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	if err := tarDir(dir, tmp); err != nil {
		return nil, fmt.Errorf("pack work dir: %w", err)
	}
	if err := s.putFile(ctx, prefix+storydomain.ArtifactWorkdir, tmp.Name()); err != nil {
		return nil, err
	}
	out[storydomain.ArtifactWorkdir] = prefix + storydomain.ArtifactWorkdir
	files := map[string]string{
		storydomain.ArtifactScript:    "script.md",
		storydomain.ArtifactMetadata:  "metadata.json",
		storydomain.ArtifactArticle:   "article.md",
		storydomain.ArtifactNarration: "narration/speech.wav",
	}
	for name, rel := range files {
		p := filepath.Join(dir, rel)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := s.putFile(ctx, prefix+name, p); err != nil {
			return nil, err
		}
		out[name] = prefix + name
	}
	return out, nil
}

func (s *Service) putFile(ctx context.Context, key, path string) error {
	f, err := os.Open(path) //nolint:gosec // our own files
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return s.blobs.Put(ctx, key, f, st.Size())
}

func (s *Service) readBlob(ctx context.Context, key string) ([]byte, error) {
	rc, _, err := s.blobs.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	var b bytes.Buffer
	_, err = b.ReadFrom(rc)
	return b.Bytes(), err
}
