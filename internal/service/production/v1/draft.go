package production

import (
	"bytes"
	"cmp"
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

	"github.com/ahmethakanbesel/ekin/internal/adapter/claude"
	"github.com/ahmethakanbesel/ekin/internal/agenttool"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	librarydomain "github.com/ahmethakanbesel/ekin/internal/domain/library/v1"
	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	voicedomain "github.com/ahmethakanbesel/ekin/internal/domain/voice/v1"
	"github.com/ahmethakanbesel/ekin/pkg/archive"
	"github.com/ahmethakanbesel/ekin/pkg/id"
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
	// Agent is the drafting model and effort, resolved at planning down
	// to the server's; empty fields leave the worker's own.
	Agent pipelinedomain.Agent `json:"agent,omitzero"`
	// Stage is what this draft makes of a video whose story reviews
	// scripts first: the script alone, or the video from the approved
	// script. Empty for every other draft.
	Stage DraftStage `json:"stage,omitempty"`
}

// DraftStage is the part of a video one draft makes.
type DraftStage string

const (
	// StageScript writes the script, narration text and metadata only,
	// for a person to approve before the video is built.
	StageScript DraftStage = "script"
	// StageFromScript builds the video from an approved script.
	StageFromScript DraftStage = "from-script"
)

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
	stage, err := s.draftStage(ctx, st, d)
	if err != nil {
		return DraftTask{}, "", s.stepFailed(ctx, p, err)
	}
	t, err := s.prepare(ctx, st, d, dir, stage)
	if err != nil {
		return DraftTask{}, "", s.stepFailed(ctx, p, err)
	}
	task := DraftTask{
		Payload: p, Spec: t.spec, Video: d.IsVideo(), Translation: d.Role() == storydomain.RoleTranslation,
		Revision: d.Revision(), Feedback: feedback, SelfReview: st.Brief().SelfReview,
		Agent: st.Brief().Production.Agent, Stage: stage, TargetWords: t.targetWords,
	}
	if t.article != nil {
		task.TargetWords = t.article.Words()
	}
	if feedback != nil || stage == StageFromScript {
		task.Resume = d.Session()
	}
	return task, dir, nil
}

// draftStage is what d's next draft makes: its script, its video from
// the script a person just approved, or (empty) the whole deliverable.
func (s *Service) draftStage(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable) (DraftStage, error) {
	if st.AtScript(d.ID()) {
		return StageScript, nil
	}
	if !d.IsVideo() || d.ScriptApprovedAt().IsZero() || d.Revision() == 0 {
		return "", nil
	}
	rev, err := s.stories.Revision(ctx, d.ID(), d.Revision())
	if err != nil {
		return "", err
	}
	if rev.Stage == storydomain.StageScript {
		return StageFromScript, nil
	}
	return "", nil
}

// RunDraft runs the agent in dir until its output validates. It touches
// no database, so a remote worker can run it.
func (s *Service) RunDraft(ctx context.Context, task DraftTask, dir string) (DraftResult, error) {
	tools := []string{"Read", "Write", "Edit", "Glob", "Grep"}
	allowed := append([]string(nil), tools...)
	if task.Video {
		tools = append(tools, "Bash")
		allowed = append(allowed, s.ekinAllowed(ctx)...)
	}
	req := claude.Request{Dir: dir, Tools: tools, Allowed: allowed, Timeout: s.cfg.AgentTimeout, Env: s.toolEnv(ctx, task.Spec),
		Model: task.Agent.Model, Effort: string(task.Agent.Effort)}
	var err error
	req.Prompt, err = draftPrompt(task)
	if err != nil {
		return DraftResult{}, err
	}
	if task.Resume != "" && (task.Feedback != nil || task.Stage == StageFromScript) {
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
		Env: a.req.Env, Prompt: prompt, Model: a.req.Model, Effort: a.req.Effort,
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

// settle sends Ekin's own check failures back to the author until the
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
		if err := s.turn(ctx, a, "Ekin's own check of your output failed:\n\n- "+strings.Join(problems, "\n- ")+
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
	measured := storydomain.Measures{Words: wordsIn(d, dir), Narrator: narratorIn(d, dir)}
	recut, err := movedClip(d, dir)
	if err != nil {
		return s.stepFailed(ctx, p, err)
	}
	var rev storydomain.Revision
	st, err = s.mutate(ctx, p.StoryID, func(ctx context.Context, st *storydomain.Story) error {
		if recut != nil {
			if err := st.Recut(p.DeliverableID, *recut, s.cfg.Shorts, s.now()); err != nil {
				return err
			}
		}
		var err error
		rev, err = st.Drafted(p.DeliverableID, artifacts, res.Session, res.Notes, measured, s.now())
		if err != nil {
			return err
		}
		if err := s.stories.PutRevision(ctx, rev); err != nil {
			return err
		}
		d, _ := st.Deliverable(p.DeliverableID)
		if rev.Stage != storydomain.StageScript {
			if err := s.enqueueHarvest(ctx, st, d, rev.Number, false); err != nil {
				return err
			}
		}
		return s.enqueueStep(ctx, st, d)
	})
	if apperror.Is(err, apperror.ValidationFailed) && recut != nil {
		return s.stepFailed(ctx, p, fmt.Errorf("the agent moved the short's cut: %w", err))
	}
	if err != nil {
		return err
	}
	s.recordUsage(ctx, st, p.DeliverableID, rev.Number, dir)
	if d, _ := st.Deliverable(p.DeliverableID); d.Status() == storydomain.StatusReview {
		detail := fmt.Sprintf("%s revision %d is ready for review", d.Kind(), rev.Number)
		if rev.Stage == storydomain.StageScript {
			detail = fmt.Sprintf("the video's script (revision %d) is ready for review", rev.Number)
		}
		s.notify(ctx, Event{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), Kind: "review", Title: st.Idea().Title,
			Detail: detail})
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

// Abandoned fails the deliverable a step job was moving forward when
// that job (jobID, of kind) ended without finishing its step: its worker
// vanished, it was cancelled or it failed outright. Otherwise the
// deliverable would wait for a job that is gone, and could neither be
// retried nor deleted. Call it inside the transaction that ends the job.
func (s *Service) Abandoned(ctx context.Context, kind string, jobID uuid.UUID, p DeliverablePayload, reason string) error {
	var step storydomain.Step
	switch {
	case kind == KindDraft:
		step = storydomain.StepDraft
	case kind == KindRender && p.Final:
		step = storydomain.StepFinal
	case kind == KindRender:
		step = storydomain.StepPreview
	case kind == KindPublish:
		step = storydomain.StepPublish
	default:
		return nil
	}
	failed := false
	st, err := s.mutate(ctx, p.StoryID, func(_ context.Context, st *storydomain.Story) error {
		var err error
		failed, err = st.Abandon(p.DeliverableID, step, jobID, reason, s.now())
		return err
	})
	if apperror.Is(err, apperror.StoryNotFound) || apperror.Is(err, apperror.NotFound) {
		return nil // deleted meanwhile
	}
	if err != nil || !failed {
		return err
	}
	d, _ := st.Deliverable(p.DeliverableID)
	s.log.WarnContext(ctx, "step job ended without finishing", slog.String("deliverable", d.ID().String()),
		slog.String("kind", kind), slog.String("reason", reason))
	s.notify(ctx, Event{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), Kind: "failed", Title: st.Idea().Title,
		Detail: fmt.Sprintf("%s failed: %s", d.Kind(), firstLine(reason))})
	return nil
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
// `ekin` link to this binary first on the agent's PATH, so the same
// instructions work on every host.
const toolCmd = "ekin tool"

// task is what the tools and the validator need to know.
type task struct {
	spec    agenttool.Spec
	article *pipelinedomain.ArticleSpec
	// targetWords is a video's spoken word target.
	targetWords int
	// clip is set for a short cut from a long video.
	clip *clipMaterial
}

func (s *Service) toolEnv(ctx context.Context, spec agenttool.Spec) []string {
	raw, _ := json.Marshal(spec)
	env := append(append([]string(nil), s.cfg.ToolEnv...), "EKIN_TOOL_SPEC="+string(raw))
	if s.cfg.ExtraEnv != nil {
		env = append(env, s.cfg.ExtraEnv(ctx)...)
	}
	if spec.Media.LibraryKinds() != nil {
		env = append(env, s.cfg.LibraryEnv...)
	}
	if dir := s.binDir(ctx); dir != "" {
		// Both: the PATH for the agent's process, and an env file Claude Code
		// sources before every shell command, so a shell that starts from
		// another PATH (a resumed session's) still finds ekin.
		env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"CLAUDE_ENV_FILE="+filepath.Join(dir, envFile))
	}
	return env
}

// envFile, beside the ekin link, puts the link's directory on PATH.
const envFile = "env.sh"

// ekinAllowed are the agent's pre-approved tool commands: `ekin tool`
// as the instructions spell it, and by its absolute paths, which an
// agent falls back to when its shell cannot find ekin.
func (s *Service) ekinAllowed(ctx context.Context) []string {
	allowed := []string{"Bash(ekin tool:*)"}
	if dir := s.binDir(ctx); dir != "" {
		allowed = append(allowed, "Bash("+filepath.Join(dir, "ekin")+" tool:*)")
	}
	if bin := s.cfg.EkinBin; bin != "" && !strings.ContainsAny(bin, " ()*") {
		allowed = append(allowed, "Bash("+bin+" tool:*)")
	}
	return allowed
}

// binDir holds a `ekin` symlink to this binary for the agent's PATH.
func (s *Service) binDir(ctx context.Context) string {
	if s.cfg.EkinBin == "" {
		return ""
	}
	// Absolute: the agent's shell runs in the deliverable's directory, so
	// a relative work root (EKIN_DATA_DIR=data) would leave ekin off it.
	dir, err := filepath.Abs(filepath.Join(s.cfg.WorkRoot, ".bin"))
	if err != nil {
		s.log.WarnContext(ctx, "cannot link ekin for the agent", slog.Any("error", err))
		return ""
	}
	link := filepath.Join(dir, "ekin")
	env := filepath.Join(dir, envFile)
	script := "export PATH=" + shellQuote(dir) + `":$PATH"` + "\n"
	if target, err := os.Readlink(link); err == nil && target == s.cfg.EkinBin {
		if b, err := os.ReadFile(env); err == nil && string(b) == script { //nolint:gosec // our own file
			return dir
		}
	}
	_ = os.MkdirAll(dir, 0o750)
	_ = os.Remove(link)
	if err := os.Symlink(s.cfg.EkinBin, link); err != nil {
		s.log.WarnContext(ctx, "cannot link ekin for the agent", slog.Any("error", err))
		return ""
	}
	if err := os.WriteFile(env, []byte(script), 0o600); err != nil {
		s.log.WarnContext(ctx, "cannot write the agent's env file", slog.Any("error", err))
	}
	return dir
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// prepare writes INSTRUCTIONS.md, BRIEF.md, KIT.md and the sources.
func (s *Service) prepare(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, dir string, stage DraftStage) (task, error) {
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
	if d.Clip() != nil {
		c, err := s.prepareClip(ctx, st, d, dir)
		if err != nil {
			return t, err
		}
		t.clip = &c
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
		t.spec.FinalScale = cmp.Or(st.Brief().Production.FinalQuality.Scale(), s.renderSettings(ctx).FinalScale)
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
		t.targetWords = int(v.Duration().Seconds() * wps)
		data["TargetWords"] = t.targetWords
		data["ScriptOnly"], data["FromScript"] = stage == StageScript, stage == StageFromScript
		t.spec.Media = st.Brief().Production.Media
		data["Media"] = mediaModes(t.spec.Media)
		if err := s.writeCatalog(ctx, st, t.spec.Media, librarydomain.OrientationOf(w, h), dir); err != nil {
			return t, err
		}
		page := "video.md"
		if t.clip != nil {
			// The narration is the long video's slice: the composition
			// covers it, plus an end card at most.
			length := t.clip.clip.Length().Seconds()
			t.spec.Clip, t.spec.MinDuration, t.spec.MaxDuration = true, length, length+agenttool.ClipTail
			t.targetWords = 0
			data["ClipStart"], data["ClipEnd"] = clock(t.clip.clip.Start()), clock(t.clip.clip.End())
			data["ClipSeconds"] = strconv.FormatFloat(length, 'f', 1, 64)
			data["TailSeconds"] = int(agenttool.ClipTail)
			data["VideoURL"] = source.videoURL
			page = "clip.md"
		}
		if t.spec.Long && t.clip == nil {
			// Shorts keep a frame of the video: the Shorts feed shows one
			// chosen in the app.
			t.spec.Thumbnails = st.Brief().Production.Thumbnails.Count()
		}
		data["Thumbnails"] = t.spec.Thumbnails
		if instructions, err = execute(page, data); err != nil {
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

// mediaSource is one kind's source as the prompts name it.
type mediaSource struct {
	Label, Kind, Mode string // Mode: fresh, library or mixed
}

// mediaModes describes the media policy for the prompts: whether any kind
// comes from the library, and each kind's source.
func mediaModes(p librarydomain.Policy) map[string]any {
	return map[string]any{
		"Library": p.LibraryKinds() != nil,
		"Sources": []mediaSource{
			{"Video clips", "video", p.Video.String()},
			{"Photos", "photo", p.Photo.String()},
			{"Music", "music", p.Music.String()},
		},
	}
}

// writeCatalog lists in the work dir the library assets the draft may use
// (agenttool.CatalogPath), or removes the list when it may use none.
func (s *Service) writeCatalog(ctx context.Context, st *storydomain.Story, policy librarydomain.Policy,
	orientation librarydomain.Orientation, dir string) error {
	p := filepath.Join(dir, agenttool.CatalogPath)
	kinds := policy.LibraryKinds()
	if kinds == nil || s.library == nil {
		_ = os.Remove(p)
		return nil
	}
	entries, err := s.library.Catalog(ctx, st.WorkspaceID(), kinds, orientation)
	if err != nil {
		return err
	}
	items := make([]agenttool.CatalogItem, 0, len(entries))
	for _, e := range entries {
		a := e.Asset
		key, ok := a.Key(librarydomain.FileMain)
		if !ok {
			continue
		}
		items = append(items, agenttool.CatalogItem{
			ID: a.ID().String(), Kind: string(a.Kind()), Title: a.Title(), Description: a.Description(), Tags: a.Tags(),
			Mood: a.Mood(), Tempo: a.Tempo(), Seconds: a.Duration().Seconds(), Width: a.Width(), Height: a.Height(),
			Uses: e.Uses, Format: a.Format(), Key: key, SHA256: a.SHA256(), Credit: a.Credit(),
		})
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, raw, 0o600)
}

// recordUsage notes the library assets a finished revision uses. Usage
// is a count for people browsing the library; failing to note it does
// not fail the draft.
func (s *Service) recordUsage(ctx context.Context, st *storydomain.Story, deliverableID uuid.UUID, revision int, dir string) {
	if s.library == nil {
		return
	}
	var ids []uuid.UUID
	for _, raw := range agenttool.LibraryIDs(dir) {
		if id, err := uuid.Parse(raw); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	if err := s.library.RecordUsage(ctx, st.WorkspaceID(), st.ID(), deliverableID, revision, ids); err != nil {
		s.log.WarnContext(ctx, "cannot record library usage", slog.Any("error", err))
	}
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
		if t.clip != nil {
			fmt.Fprintf(&b, "- **Cut from:** the approved long video (`sources/lead.md`), %s–%s; its narration is in `narration/` and `sources/clip.md`.\n",
				clock(t.clip.clip.Start()), clock(t.clip.clip.End()))
		} else {
			b.WriteString("- **Derived:** build this from the approved lead in `sources/lead.md`; stay consistent with it.\n")
		}
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
	if t.Stage == StageScript {
		return validateScript(t, dir)
	}
	if t.Video {
		if t.Spec.Thumbnails > 0 && t.Feedback.ThumbnailOnly() {
			return agenttool.CheckThumbnails(ctx, s.renderer, dir, t.Spec.Thumbnails, 0, false).Errors
		}
		rep := agenttool.Check(ctx, s.renderer, dir, "composition.html", t.Spec)
		if len(rep.Errors) > 0 || t.Spec.Thumbnails == 0 {
			return rep.Errors
		}
		return agenttool.CheckThumbnails(ctx, s.renderer, dir, t.Spec.Thumbnails, 0, false).Errors
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

// validateScript checks a script-only draft: the script, the narration
// text near its word target and the metadata a reviewer judges.
func validateScript(t DraftTask, dir string) []string {
	var problems []string
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))) //nolint:gosec // our work dir
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if read("script.md") == "" {
		problems = append(problems, "script.md is missing or empty")
	}
	if narration := read(narrationFile); narration == "" {
		problems = append(problems, narrationFile+" is missing or empty")
	} else if words := countWords(narration); t.TargetWords > 0 && (words < t.TargetWords/2 || words > t.TargetWords*3/2) {
		problems = append(problems, fmt.Sprintf("the narration has %d words; the target is about %d", words, t.TargetWords))
	}
	var meta struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(read(metadataFile)), &meta); err != nil || strings.TrimSpace(meta.Title) == "" {
		problems = append(problems, metadataFile+" is missing or has no title")
	}
	return problems
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
	case fb != nil && task.Resume != "" && task.Stage == StageScript:
		return execute("revise-script.md", map[string]any{
			"Revision": task.Revision, "Count": len(fb.Notes), "Notes": feedbackList(fb), "Tool": toolCmd,
		})
	case task.Stage == StageFromScript && task.Resume != "":
		return "A reviewer approved your script and narration. INSTRUCTIONS.md now covers building the video " +
			"from them: read it again, then do the task it describes.", nil
	case fb != nil && task.Resume != "" && task.Spec.Thumbnails > 0 && fb.ThumbnailOnly():
		return execute("revise-thumbnail.md", map[string]any{
			"Revision": task.Revision, "Count": len(fb.Notes), "Notes": feedbackList(fb), "Tool": toolCmd,
		})
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

// archivePrefix is where one attempt at d's next revision is stored.
func archivePrefix(st *storydomain.Story, d *storydomain.Deliverable) string {
	return fmt.Sprintf("ws/%s/stories/%s/%s/r%d/%s/", st.WorkspaceID(), st.ID(), d.ID(), d.Revision()+1, id.New())
}

// archive uploads the working directory and the key outputs of the
// next revision and returns their blob keys. Each call writes under a
// prefix of its own: two attempts at the same revision (two reviewers
// editing at once) both upload before only one of them is saved, and the
// other must not overwrite the files the saved one points at.
func (s *Service) archive(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, dir string) (map[string]string, error) {
	prefix := archivePrefix(st, d)
	out := map[string]string{}
	if err := os.WriteFile(filepath.Join(dir, revisionMarker), []byte(strconv.Itoa(d.Revision()+1)), 0o600); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "ekin-workdir-*.tar.gz")
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
		storydomain.ArtifactScript:        "script.md",
		storydomain.ArtifactMetadata:      "metadata.json",
		storydomain.ArtifactArticle:       "article.md",
		storydomain.ArtifactNarration:     "narration/speech.wav",
		storydomain.ArtifactNarrationText: narrationFile,
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
