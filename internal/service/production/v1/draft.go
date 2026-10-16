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
	"strconv"
	"strings"
	"text/template"

	"github.com/google/uuid"

	"kurgu/internal/adapter/claude"
	"kurgu/internal/agenttool"
	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	voicedomain "kurgu/internal/domain/voice/v1"
	"kurgu/pkg/archive"
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

// DraftTask is a self-contained agent job: with the working directory it
// is everything RunDraft needs, so it can run on another host (the
// hybrid worker) as well as here.
type DraftTask struct {
	Payload     DeliverablePayload    `json:"payload"`
	Spec        agenttool.Spec        `json:"spec"`
	Video       bool                  `json:"video"`
	Translation bool                  `json:"translation"`
	TargetWords int                   `json:"targetWords,omitempty"`
	Revision    int                   `json:"revision"` // before this draft
	Resume      string                `json:"resume,omitempty"`
	Feedback    *storydomain.Feedback `json:"feedback,omitempty"`
	// SelfReview is the most self-review rounds (story brief); 0 for none.
	SelfReview int `json:"selfReview,omitempty"`
}

// DraftResult is what the agent produced besides files.
type DraftResult struct {
	Session string `json:"session"`
	Notes   string `json:"notes"`
}

// Draft runs a deliverable.draft job here: begin, run the agent, finish.
func (s *Service) Draft(ctx context.Context, p DeliverablePayload) error {
	task, dir, err := s.BeginDraft(ctx, p)
	if err != nil || dir == "" {
		return err
	}
	res, err := s.RunDraft(ctx, task, dir)
	if err != nil {
		return s.stepFailed(ctx, p, err)
	}
	return s.FinishDraft(ctx, task, dir, res)
}

// BeginDraft hands the deliverable to the agent: it marks it drafting
// and prepares its working directory. An empty dir means there is
// nothing to do (the deliverable moved on).
func (s *Service) BeginDraft(ctx context.Context, p DeliverablePayload) (DraftTask, string, error) {
	var feedback *storydomain.Feedback
	st, err := s.mutate(ctx, p.StoryID, func(_ context.Context, st *storydomain.Story) error {
		var err error
		feedback, err = st.StartDrafting(p.DeliverableID, s.now())
		return err
	})
	if apperror.Is(err, apperror.InvalidTransition) || apperror.Is(err, apperror.StoryNotFound) {
		return DraftTask{}, "", jobdomain.Permanent(err) // already handled, rejected or deleted
	}
	if err != nil {
		return DraftTask{}, "", err
	}
	d, _ := st.Deliverable(p.DeliverableID)
	dir := s.workDir(d)
	if err := s.ensureWorkDir(ctx, d, dir); err != nil {
		return DraftTask{}, "", s.stepFailed(ctx, p, err)
	}
	t, err := s.prepare(ctx, st, d, dir)
	if err != nil {
		return DraftTask{}, "", s.stepFailed(ctx, p, err)
	}
	task := DraftTask{
		Payload: p, Spec: t.spec, Video: d.IsVideo(), Translation: d.Role() == storydomain.RoleTranslation,
		Revision: d.Revision(), Feedback: feedback, SelfReview: st.Brief().SelfReview,
	}
	if t.article != nil {
		task.TargetWords = t.article.Words()
	}
	if feedback != nil {
		task.Resume = d.Session()
	}
	return task, dir, nil
}

// RunDraft runs the agent in dir until its output validates. It touches
// no database, so a remote worker can run it.
func (s *Service) RunDraft(ctx context.Context, task DraftTask, dir string) (DraftResult, error) {
	tools := []string{"Read", "Write", "Edit", "Glob", "Grep"}
	allowed := append([]string(nil), tools...)
	if task.Video {
		tools = append(tools, "Bash")
		allowed = append(allowed, "Bash(kurgu tool:*)")
	}
	req := claude.Request{Dir: dir, Tools: tools, Allowed: allowed, Timeout: s.cfg.AgentTimeout, Env: s.toolEnv(ctx, task.Spec)}
	var err error
	req.Prompt, err = draftPrompt(task)
	if err != nil {
		return DraftResult{}, err
	}
	if task.Feedback != nil && task.Resume != "" {
		req.Resume = task.Resume
	}
	res, err := s.agent.Run(ctx, req)
	if err != nil {
		return DraftResult{}, err
	}
	a := author{req: req, session: res.SessionID, reply: res.Text}
	if err := s.settle(ctx, task, dir, &a); err != nil {
		return DraftResult{}, err
	}
	notes := strings.TrimSpace(a.reply)
	review, err := s.selfReview(ctx, task, dir, &a)
	if err != nil {
		return DraftResult{}, err
	}
	return DraftResult{Session: a.session, Notes: strings.TrimSpace(notes + review)}, nil
}

// author is the drafting session, which later turns resume.
type author struct {
	req     claude.Request // the first turn's tools and environment
	session string
	reply   string // the latest reply
}

// turn resumes the author with prompt.
func (s *Service) turn(ctx context.Context, a *author, prompt string) error {
	res, err := s.agent.Run(ctx, claude.Request{
		Dir: a.req.Dir, Tools: a.req.Tools, Allowed: a.req.Allowed, Timeout: s.cfg.AgentTimeout, Resume: a.session,
		Env: a.req.Env, Prompt: prompt,
	})
	if err != nil {
		return err
	}
	if res.SessionID != "" {
		a.session = res.SessionID
	}
	a.reply = res.Text
	return nil
}

// settle sends Kurgu's own check failures back to the author until the
// output passes, at most FixAttempts times.
func (s *Service) settle(ctx context.Context, task DraftTask, dir string, a *author) error {
	for attempt := 0; ; attempt++ {
		problems := s.validate(ctx, task, dir)
		if len(problems) == 0 {
			return nil
		}
		if attempt >= s.cfg.FixAttempts {
			return fmt.Errorf("output still invalid after %d fixes: %s", attempt, strings.Join(problems, "; "))
		}
		if err := s.turn(ctx, a, "Kurgu's own check of your output failed:\n\n- "+strings.Join(problems, "\n- ")+
			"\n\nFix every item, check again, and reply when done."); err != nil {
			return err
		}
	}
}

// FinishDraft archives dir as the next revision and moves the
// deliverable on (to rendering or review).
func (s *Service) FinishDraft(ctx context.Context, task DraftTask, dir string, res DraftResult) error {
	p := task.Payload
	st, err := s.stories.GetByID(ctx, p.StoryID)
	if err != nil {
		return err
	}
	d, err := st.Deliverable(p.DeliverableID)
	if err != nil {
		return jobdomain.Permanent(err)
	}
	artifacts, err := s.archive(ctx, st, d, dir)
	if err != nil {
		return s.stepFailed(ctx, p, err)
	}
	words := wordsIn(d, dir)
	var rev storydomain.Revision
	st, err = s.mutate(ctx, p.StoryID, func(ctx context.Context, st *storydomain.Story) error {
		var err error
		rev, err = st.Drafted(p.DeliverableID, artifacts, res.Session, res.Notes, words, s.now())
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

// stepFailed ends a job whose step failed. When ctx ended (the worker
// is stopping) the step did not fail, it was cut short: the error goes
// back to the runner, which releases the job to run again. Otherwise the
// deliverable is marked failed and the job is done.
func (s *Service) stepFailed(ctx context.Context, p DeliverablePayload, cause error) error {
	if ctx.Err() != nil {
		return cause
	}
	s.FailStep(ctx, p, cause)
	return nil
}

// FailStep marks the deliverable's current step failed.
func (s *Service) FailStep(ctx context.Context, p DeliverablePayload, cause error) {
	st, err := s.stories.GetByID(ctx, p.StoryID)
	if err != nil {
		return
	}
	d, err := st.Deliverable(p.DeliverableID)
	if err != nil {
		return
	}
	s.log.ErrorContext(ctx, "step failed", slog.String("deliverable", d.ID().String()), slog.String("status", string(d.Status())), slog.Any("error", cause))
	s.fail(ctx, st, d, cause)
}

// WorkDir is the local working directory of a deliverable.
func (s *Service) WorkDir(deliverableID uuid.UUID) string {
	return filepath.Join(s.cfg.WorkRoot, deliverableID.String())
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

// toolCmd is how instructions name the agent tools; RunDraft puts a
// `kurgu` link to this binary first on the agent's PATH, so the same
// instructions work on every host.
const toolCmd = "kurgu tool"

// task is what the tools and the validator need to know.
type task struct {
	spec    agenttool.Spec
	article *pipelinedomain.ArticleSpec
}

func (s *Service) toolEnv(ctx context.Context, spec agenttool.Spec) []string {
	raw, _ := json.Marshal(spec)
	env := append(append([]string(nil), s.cfg.ToolEnv...), "KURGU_TOOL_SPEC="+string(raw))
	if s.cfg.ExtraEnv != nil {
		env = append(env, s.cfg.ExtraEnv(ctx)...)
	}
	if dir := s.binDir(ctx); dir != "" {
		env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return env
}

// binDir holds a `kurgu` symlink to this binary for the agent's PATH.
func (s *Service) binDir(ctx context.Context) string {
	if s.cfg.KurguBin == "" {
		return ""
	}
	dir := filepath.Join(s.cfg.WorkRoot, ".bin")
	link := filepath.Join(dir, "kurgu")
	if target, err := os.Readlink(link); err == nil && target == s.cfg.KurguBin {
		return dir
	}
	_ = os.MkdirAll(dir, 0o750)
	_ = os.Remove(link)
	if err := os.Symlink(s.cfg.KurguBin, link); err != nil {
		s.log.WarnContext(ctx, "cannot link kurgu for the agent", slog.Any("error", err))
		return ""
	}
	return dir
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
		"Tool": toolCmd,
	}
	// A language with a writing guide (writing-<code>.md) gets it as
	// WRITING.md: how its native speakers would say it, not a translation.
	_ = os.Remove(filepath.Join(dir, "WRITING.md"))
	if prompts.Lookup("writing-"+string(d.Language())+".md") != nil {
		guide, err := execute("writing-"+string(d.Language())+".md", data)
		if err != nil {
			return t, err
		}
		if err := write("WRITING.md", guide); err != nil {
			return t, err
		}
		data["HasWriting"] = true
	}
	switch {
	case d.IsVideo():
		v := *d.Video()
		w, h := videoFrame(v)
		lo, hi := durationBounds(v)
		t.spec.Width, t.spec.Height, t.spec.MinDuration, t.spec.MaxDuration = w, h, lo, hi
		t.spec.Long = v.Format() == pipelinedomain.FormatLong
		if s.voices != nil {
			var override *voicedomain.Override
			if d.Language() == st.Language() {
				override = st.Brief().Voice
			}
			opts, err := s.voices.Options(ctx, st.WorkspaceID(), string(d.Language()), override)
			if err != nil {
				return t, err
			}
			t.spec.Speech = opts
		}
		data["Long"] = v.Format() == pipelinedomain.FormatLong
		data["Width"], data["Height"] = w, h
		data["MinSeconds"], data["MaxSeconds"] = int(lo), int(hi)
		wps := wordsPerSecond(d.Language())
		data["WordsPerSecond"] = strconv.FormatFloat(wps, 'f', 1, 64)
		data["TargetWords"] = int(v.Duration().Seconds() * wps)
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
	_ = os.Remove(filepath.Join(dir, "STYLE.md"))
	if tmpl := st.Brief().Template; tmpl.Applies(string(d.Kind())) {
		if err := write("STYLE.md", styleDoc(tmpl)); err != nil {
			return t, err
		}
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
	if kw := st.Brief().Keyword; kw != "" {
		fmt.Fprintf(&b, "\n**Target search keyword:** %q. Use it naturally in the title, in the first sentence, and in the description; answer exactly what someone searching it wants to know.\n", kw)
	}
	if ins := strings.TrimSpace(st.Brief().Instructions); ins != "" {
		fmt.Fprintf(&b, "\n## Standing instructions\n\n%s\n", ins)
	}
	if tmpl := st.Brief().Template; tmpl.Applies(string(d.Kind())) {
		fmt.Fprintf(&b, "\n## House style\n\nFollow `STYLE.md` (template %q).\n", tmpl.Name)
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
func (s *Service) validate(ctx context.Context, t DraftTask, dir string) []string {
	if t.Video {
		rep := agenttool.Check(ctx, s.renderer, dir, "composition.html", t.Spec)
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
	if t.TargetWords > 0 && !t.Translation {
		target := t.TargetWords
		if a.Words < target*6/10 || a.Words > target*16/10 {
			return []string{fmt.Sprintf("the body has %d words; the target is about %d", a.Words, target)}
		}
	}
	return nil
}

// wordsPerSecond is the narration pace scripts are written for: brisk and
// conversational, like the explainer channels Shorts viewers stay for
// (about 170 words a minute in English). Turkish words are longer, so
// the same pace is fewer of them.
func wordsPerSecond(l pipelinedomain.Language) float64 {
	if l == pipelinedomain.English {
		return 2.8
	}
	return 2.0
}

// draftPrompt is the agent's first turn: the brief for a first draft,
// or the reviewer's notes, resuming the session that wrote the draft
// (or, without one, over the previous version's files).
func draftPrompt(task DraftTask) (string, error) {
	fb := task.Feedback
	switch {
	case fb != nil && task.Resume != "":
		return execute("revise.md", map[string]any{
			"Revision": task.Revision, "Count": len(fb.Notes), "Notes": feedbackList(fb), "Tool": toolCmd,
		})
	case fb != nil:
		return "Read INSTRUCTIONS.md, then BRIEF.md, and do the task they describe.\n\n" +
			"A reviewer asked for these changes to the previous version (kept in this directory); address every one:\n\n" +
			feedbackList(fb), nil
	}
	return "Read INSTRUCTIONS.md, then BRIEF.md, and do the task they describe.", nil
}

// feedbackList is the reviewer's notes as a numbered Markdown list, in
// reading order: what each is about and where, then the note quoted.
func feedbackList(fb *storydomain.Feedback) string {
	var b strings.Builder
	for i, n := range fb.Notes {
		var about string
		switch {
		case scopeName(n.Scope) != "":
			about = "The " + scopeName(n.Scope)
		case n.AtSeconds == nil:
			about = "The whole draft"
		}
		if n.AtSeconds != nil {
			if about == "" {
				about = "At"
			} else {
				about += ", at"
			}
			about += fmt.Sprintf(" about %.1f s", *n.AtSeconds)
		}
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%d. %s:\n", i+1, about)
		for line := range strings.SplitSeq(n.Text, "\n") {
			b.WriteString("   > " + strings.TrimRight(line, " \t") + "\n")
		}
	}
	return b.String()
}

func scopeName(sc storydomain.Scope) string {
	if sc == storydomain.ScopeAll || sc == "" {
		return ""
	}
	return string(sc)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// ensureWorkDir makes dir hold the deliverable's latest revision. A host
// keeps its work dir between steps, but another host (a hybrid worker,
// or the server for an in-place edit) may have made a newer revision
// since, so the dir is restored when its revision marker is behind.
func (s *Service) ensureWorkDir(ctx context.Context, d *storydomain.Deliverable, dir string) error {
	if _, err := os.Stat(dir); err == nil {
		raw, err := os.ReadFile(filepath.Join(dir, revisionMarker)) //nolint:gosec // our work dir
		if err != nil || strings.TrimSpace(string(raw)) == strconv.Itoa(d.Revision()) {
			return nil // current, or from before markers existed
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if d.Revision() == 0 {
		return nil
	}
	return s.restore(ctx, d, dir)
}

// restore unpacks the deliverable's latest revision into dir.
func (s *Service) restore(ctx context.Context, d *storydomain.Deliverable, dir string) error {
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
	return archive.Untar(rc, dir)
}

// archive uploads the working directory and the key outputs of this
// revision and returns their blob keys.
func (s *Service) archive(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, dir string) (map[string]string, error) {
	prefix := fmt.Sprintf("ws/%s/stories/%s/%s/r%d/", st.WorkspaceID(), st.ID(), d.ID(), d.Revision()+1)
	out := map[string]string{}
	if err := os.WriteFile(filepath.Join(dir, revisionMarker), []byte(strconv.Itoa(d.Revision()+1)), 0o600); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "kurgu-workdir-*.tar.gz")
	if err != nil {
		return nil, err
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	if err := archive.TarGz(dir, tmp); err != nil {
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

// styleDoc renders a template as STYLE.md for the agent.
func styleDoc(t *storydomain.TemplateStyle) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# House style: %s\n\n", t.Name)
	if len(t.Palette) > 0 {
		fmt.Fprintf(&b, "- **Palette:** %s (use these colours; the first is the background)\n", strings.Join(t.Palette, ", "))
	}
	if t.HeadingFont != "" {
		fmt.Fprintf(&b, "- **Fonts:** headings %s, body and captions %s\n", t.HeadingFont, t.BodyFont)
	}
	if t.MusicMood != "" {
		fmt.Fprintf(&b, "- **Music:** %s\n", t.MusicMood)
	}
	b.WriteString("\n" + strings.TrimSpace(t.Guide) + "\n\n")
	b.WriteString("## Always\n\n")
	b.WriteString("- This style may be inspired by a well-known channel. Make it recognisably our own: never use any channel's name, logo, mascot, characters, catchphrases, music or title cards.\n")
	b.WriteString("- The brief's audience rules win over this style when they conflict.\n")
	return b.String()
}
