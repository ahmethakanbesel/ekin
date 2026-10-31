// Package visibility checks how answer engines talk about a workspace's
// brand: it keeps the prompts buyers would ask (the agent drafts them,
// people edit them), runs every active prompt against every configured
// OpenRouter model within a monthly call cap, reads each answer for the
// brand, its rank, citations and competitors, and reports where the
// brand is missing so pipelines can cover it.
package visibility

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/adapter/claude"
	"github.com/ahmethakanbesel/ekin/internal/adapter/openrouter"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	visibilitydomain "github.com/ahmethakanbesel/ekin/internal/domain/visibility/v1"
	visibilityrepo "github.com/ahmethakanbesel/ekin/internal/repository/visibility/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// Job kinds.
const (
	KindRun   = "visibility.run"
	KindDraft = "visibility.draft"
)

// RunPayload addresses a run.
type RunPayload struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	RunID       uuid.UUID `json:"runId"`
}

// DraftPayload asks the agent for Count prompts.
type DraftPayload struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	Count       int       `json:"count"`
}

// DefaultModels are offered when a workspace sets the checks up: answer
// engines that search the web, so their answers cite sources.
var DefaultModels = []string{"perplexity/sonar", "openai/gpt-4o-mini:online", "google/gemini-2.5-flash:online"}

// Asker asks a model a question (internal/adapter/openrouter).
type Asker interface {
	Enabled() bool
	Ask(ctx context.Context, model, prompt string) (openrouter.Answer, error)
}

// Agent drafts prompts (internal/adapter/claude).
type Agent interface {
	Run(ctx context.Context, req claude.Request) (claude.Result, error)
}

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// Context is what the workspace already knows that a profile and its
// prompts start from: the site's domain, the SEO profile's competitors
// and topics, the content language.
type Context struct {
	Domain      string
	Competitors []string // domains
	Topics      string
	Language    string // en | tr
}

// Service runs visibility use cases.
type Service struct {
	repo    visibilityrepo.Repository
	tx      db.TxManager
	jobs    Enqueuer
	asker   func(ctx context.Context) Asker
	agent   Agent
	known   func(ctx context.Context, ws uuid.UUID) Context
	workDir string
	log     *slog.Logger
	now     func() time.Time
}

// Deps are the collaborators; Agent and Known are optional.
type Deps struct {
	Repo    visibilityrepo.Repository
	Tx      db.TxManager
	Jobs    Enqueuer
	Asker   func(ctx context.Context) Asker
	Agent   Agent
	Known   func(ctx context.Context, ws uuid.UUID) Context
	WorkDir string // where the drafting agent runs
	Log     *slog.Logger
}

// NewService returns a Service.
func NewService(d Deps) *Service {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	known := d.Known
	if known == nil {
		known = func(context.Context, uuid.UUID) Context { return Context{} }
	}
	return &Service{repo: d.Repo, tx: d.Tx, jobs: d.Jobs, asker: d.Asker, agent: d.Agent, known: known, workDir: d.WorkDir,
		log: log, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Overview is a workspace's checks at a glance.
type Overview struct {
	Profile *visibilitydomain.Profile // nil until set up
	// Suggested are settings to start from when there is no profile.
	Suggested visibilitydomain.Settings
	Prompts   []visibilitydomain.Prompt
	Runs      []visibilitydomain.Run // newest first
	Estimate  Estimate
	// Ready reports whether the server can ask models (an OpenRouter key).
	Ready bool
}

// Estimate is what the next run would cost in calls, against the month.
type Estimate struct {
	Prompts, Models, Calls int
	Used, Cap              int
}

// Overview returns the workspace's checks.
func (s *Service) Overview(ctx context.Context, ws uuid.UUID) (Overview, error) {
	o := Overview{Ready: s.asker(ctx).Enabled()}
	p, err := s.repo.Profile(ctx, ws)
	switch {
	case apperror.Is(err, apperror.NotFound):
		o.Suggested = s.suggest(ctx, ws)
	case err != nil:
		return o, err
	default:
		o.Profile = p
	}
	if o.Prompts, err = s.repo.Prompts(ctx, ws); err != nil {
		return o, err
	}
	if o.Runs, err = s.repo.Runs(ctx, ws, 12); err != nil {
		return o, err
	}
	o.Estimate, err = s.estimate(ctx, ws, o.Profile, o.Prompts)
	return o, err
}

func (s *Service) suggest(ctx context.Context, ws uuid.UUID) visibilitydomain.Settings {
	k := s.known(ctx, ws)
	st := visibilitydomain.Settings{Models: slices.Clone(DefaultModels), Schedule: visibilitydomain.ScheduleWeekly,
		MonthlyCap: visibilitydomain.DefaultCap}
	if d, err := visibilitydomain.ParseDomain(k.Domain); err == nil && d != "" {
		st.Brand = visibilitydomain.Brand{Name: nameOf(d), Domain: d}
	}
	for _, c := range k.Competitors {
		if d, err := visibilitydomain.ParseDomain(c); err == nil && d != "" && d != st.Brand.Domain && len(st.Competitors) < visibilitydomain.MaxCompetitors {
			st.Competitors = append(st.Competitors, visibilitydomain.Brand{Name: nameOf(d), Domain: d})
		}
	}
	return st
}

// nameOf guesses a brand name from its domain: "acme-tools.com" → "Acme Tools".
func nameOf(domain string) string {
	label, _, _ := strings.Cut(domain, ".")
	words := strings.FieldsFunc(label, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func (s *Service) estimate(ctx context.Context, ws uuid.UUID, p *visibilitydomain.Profile, prompts []visibilitydomain.Prompt) (Estimate, error) {
	e := Estimate{Cap: visibilitydomain.DefaultCap}
	for _, pr := range prompts {
		if pr.Active {
			e.Prompts++
		}
	}
	if p != nil {
		e.Models, e.Cap = len(p.Settings().Models), p.Settings().MonthlyCap
	}
	e.Calls = e.Prompts * e.Models
	var err error
	e.Used, err = s.repo.CallsSince(ctx, ws, visibilitydomain.MonthStart(s.now()))
	return e, err
}

// SaveSettings sets the checks up or changes them.
func (s *Service) SaveSettings(ctx context.Context, ws uuid.UUID, st visibilitydomain.Settings) (*visibilitydomain.Profile, error) {
	var out *visibilitydomain.Profile
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.repo.Profile(ctx, ws)
		switch {
		case apperror.Is(err, apperror.NotFound):
			if p, err = visibilitydomain.NewProfile(ws, st, s.now()); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := p.Update(st, s.now()); err != nil {
				return err
			}
		}
		out = p
		return s.repo.PutProfile(ctx, p)
	})
	return out, err
}

// AddPrompt adds a person's prompt.
func (s *Service) AddPrompt(ctx context.Context, ws uuid.UUID, text string) (visibilitydomain.Prompt, error) {
	text, err := visibilitydomain.ParsePromptText(text)
	if err != nil {
		return visibilitydomain.Prompt{}, err
	}
	var p visibilitydomain.Prompt
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		list, err := s.repo.Prompts(ctx, ws)
		if err != nil {
			return err
		}
		if len(list) >= visibilitydomain.MaxPrompts {
			return apperror.New(apperror.ValidationFailed, fmt.Sprintf("a workspace keeps at most %d prompts", visibilitydomain.MaxPrompts))
		}
		if slices.ContainsFunc(list, func(x visibilitydomain.Prompt) bool { return strings.EqualFold(x.Text, text) }) {
			return apperror.New(apperror.Conflict, "that prompt is already in the set")
		}
		now := s.now()
		p = visibilitydomain.Prompt{ID: id.New(), WorkspaceID: ws, Text: text, Source: visibilitydomain.SourcePerson, Active: true,
			CreatedAt: now, UpdatedAt: now}
		return s.repo.PutPrompt(ctx, p)
	})
	return p, err
}

// UpdatePrompt rewords a prompt or turns it on or off.
func (s *Service) UpdatePrompt(ctx context.Context, ws, promptID uuid.UUID, text *string, active *bool) (visibilitydomain.Prompt, error) {
	var p visibilitydomain.Prompt
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if p, err = s.repo.Prompt(ctx, ws, promptID); err != nil {
			return err
		}
		if text != nil {
			if p.Text, err = visibilitydomain.ParsePromptText(*text); err != nil {
				return err
			}
		}
		if active != nil {
			p.Active = *active
		}
		p.UpdatedAt = s.now()
		return s.repo.PutPrompt(ctx, p)
	})
	return p, err
}

// DeletePrompt removes a prompt and what was read from its answers.
func (s *Service) DeletePrompt(ctx context.Context, ws, promptID uuid.UUID) error {
	return s.repo.DeletePrompt(ctx, ws, promptID)
}

// QueueDraft asks the agent for count more prompts.
func (s *Service) QueueDraft(ctx context.Context, ws uuid.UUID, count int) (*jobdomain.Job, error) {
	if s.agent == nil {
		return nil, apperror.New(apperror.ValidationFailed, "drafting prompts needs the authoring agent on this server")
	}
	if _, err := s.repo.Profile(ctx, ws); err != nil {
		if apperror.Is(err, apperror.NotFound) {
			return nil, apperror.New(apperror.ValidationFailed, "set the brand and competitors up first")
		}
		return nil, err
	}
	if count < 1 || count > 20 {
		return nil, apperror.New(apperror.ValidationFailed, "draft between 1 and 20 prompts")
	}
	payload, _ := json.Marshal(DraftPayload{WorkspaceID: ws, Count: count})
	j, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{WorkspaceID: ws, Queue: jobdomain.QueueAgent, Kind: KindDraft, Payload: payload, MaxAttempts: 2})
	return j, err
}

var promptsSchema = json.RawMessage(`{
  "type": "object",
  "required": ["prompts"],
  "properties": {
    "prompts": {
      "type": "array",
      "items": {"type": "string", "description": "one question, as a buyer would type it to an AI assistant"}
    }
  }
}`)

// RunDraft runs a visibility.draft job: the agent writes prompts from the
// brand, competitors and topics, and the new ones join the set (active).
func (s *Service) RunDraft(ctx context.Context, p DraftPayload) error {
	prof, err := s.repo.Profile(ctx, p.WorkspaceID)
	if err != nil {
		return jobdomain.Permanent(err)
	}
	existing, err := s.repo.Prompts(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	st, k := prof.Settings(), s.known(ctx, p.WorkspaceID)
	var b strings.Builder
	fmt.Fprintf(&b, "Write %d prompts that a potential buyer of %s", p.Count, st.Brand.Name)
	if st.Brand.Domain != "" {
		fmt.Fprintf(&b, " (%s)", st.Brand.Domain)
	}
	b.WriteString(" would type into an AI assistant such as ChatGPT or Perplexity while looking for a solution. ")
	b.WriteString("Write them as real people do: questions about their problem, best options, comparisons and alternatives, ")
	b.WriteString("pricing and fit for their situation. Most must not name the brand: the point is to see whether answers bring it up. ")
	b.WriteString("Some may name a competitor (\"alternatives to X\"). Each prompt stands alone and is under 200 characters.\n\n")
	if k.Topics != "" {
		fmt.Fprintf(&b, "What the business is about: %s\n", k.Topics)
	}
	if len(st.Competitors) > 0 {
		names := make([]string, 0, len(st.Competitors))
		for _, c := range st.Competitors {
			names = append(names, c.Name)
		}
		fmt.Fprintf(&b, "Competitors: %s\n", strings.Join(names, ", "))
	}
	if k.Language == "tr" {
		b.WriteString("Write the prompts in Turkish.\n")
	}
	if len(existing) > 0 {
		b.WriteString("\nAlready in the set; do not repeat these or near-variants:\n")
		for _, e := range existing {
			b.WriteString("- " + e.Text + "\n")
		}
	}
	res, err := s.agent.Run(ctx, claude.Request{Dir: s.workDir, Prompt: b.String(), JSONSchema: promptsSchema, Timeout: 10 * time.Minute})
	if err != nil {
		return fmt.Errorf("draft prompts: %w", err)
	}
	var out struct {
		Prompts []string `json:"prompts"`
	}
	if err := json.Unmarshal(res.Structured, &out); err != nil {
		return fmt.Errorf("draft prompts: decode: %w", err)
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		list, err := s.repo.Prompts(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		now := s.now()
		for i, raw := range out.Prompts {
			if len(list) >= visibilitydomain.MaxPrompts || i >= p.Count {
				break
			}
			text, err := visibilitydomain.ParsePromptText(raw)
			if err != nil || slices.ContainsFunc(list, func(x visibilitydomain.Prompt) bool { return strings.EqualFold(x.Text, text) }) {
				continue
			}
			pr := visibilitydomain.Prompt{ID: id.New(), WorkspaceID: p.WorkspaceID, Text: text, Source: visibilitydomain.SourceAgent,
				Active: true, CreatedAt: now.Add(time.Duration(i) * time.Millisecond), UpdatedAt: now}
			if err := s.repo.PutPrompt(ctx, pr); err != nil {
				return err
			}
			list = append(list, pr)
		}
		return nil
	})
}

// staleRun is how long a run may stay unfinished before another may
// start: its worker is taken as gone.
const staleRun = 3 * time.Hour

// StartRun queues a run of every active prompt against every model,
// refused when it would pass the month's cap or one is under way.
func (s *Service) StartRun(ctx context.Context, ws uuid.UUID, trigger string) (visibilitydomain.Run, error) {
	if !s.asker(ctx).Enabled() {
		return visibilitydomain.Run{}, apperror.New(apperror.ValidationFailed, "asking models needs an OpenRouter API key (System › Credentials)")
	}
	var run visibilitydomain.Run
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.repo.Profile(ctx, ws)
		if apperror.Is(err, apperror.NotFound) {
			return apperror.New(apperror.ValidationFailed, "set the brand and the models up first")
		}
		if err != nil {
			return err
		}
		latest, err := s.repo.Runs(ctx, ws, 1)
		if err != nil {
			return err
		}
		if len(latest) == 1 && !latest[0].Finished() && s.now().Sub(latest[0].CreatedAt) < staleRun {
			return apperror.New(apperror.Conflict, "a check is already running")
		}
		prompts, err := s.repo.Prompts(ctx, ws)
		if err != nil {
			return err
		}
		e, err := s.estimate(ctx, ws, p, prompts)
		if err != nil {
			return err
		}
		if err := visibilitydomain.Allow(e.Calls, e.Used, e.Cap); err != nil {
			return err
		}
		run = visibilitydomain.Run{ID: id.New(), WorkspaceID: ws, Status: visibilitydomain.RunQueued, Trigger: trigger, Planned: e.Calls,
			CreatedAt: s.now()}
		if err := s.repo.CreateRun(ctx, run); err != nil {
			return err
		}
		p.Ran(s.now())
		if err := s.repo.PutProfile(ctx, p); err != nil {
			return err
		}
		payload, _ := json.Marshal(RunPayload{WorkspaceID: ws, RunID: run.ID})
		_, _, err = s.jobs.Enqueue(ctx, jobdomain.Spec{WorkspaceID: ws, Queue: jobdomain.QueueIO, Kind: KindRun, Payload: payload,
			MaxAttempts: 3, ConcurrencyKey: KindRun + ":" + ws.String()})
		return err
	})
	return run, err
}

// TriggerDue starts the scheduled runs that are due, and forgets answer
// texts past their retention. It returns how many runs it started.
func (s *Service) TriggerDue(ctx context.Context) (int, error) {
	if n, err := s.repo.ForgetTexts(ctx, s.now().Add(-visibilitydomain.AnswerRetention)); err != nil {
		s.log.WarnContext(ctx, "cannot forget old answers", slog.Any("error", err))
	} else if n > 0 {
		s.log.InfoContext(ctx, "forgot old answer texts", slog.Int("answers", n))
	}
	due, err := s.repo.DueProfiles(ctx, s.now())
	if err != nil {
		return 0, err
	}
	started := 0
	for _, p := range due {
		if _, err := s.StartRun(ctx, p.WorkspaceID(), "schedule"); err != nil {
			s.log.WarnContext(ctx, "scheduled visibility check not started", slog.String("workspace", p.WorkspaceID().String()),
				slog.String("reason", apperror.Message(err)))
			// Skip this slot rather than retrying every tick.
			_ = s.tx.WithinTx(ctx, func(ctx context.Context) error {
				p.Ran(s.now())
				return s.repo.PutProfile(ctx, p)
			})
			continue
		}
		started++
	}
	return started, nil
}

// RunCheck runs a visibility.run job: each active prompt asked of each
// model, one call at a time, each answer read and kept. A retried job
// skips the calls already made; the month's cap stops it.
func (s *Service) RunCheck(ctx context.Context, p RunPayload) error {
	run, err := s.repo.Run(ctx, p.WorkspaceID, p.RunID)
	if apperror.Is(err, apperror.NotFound) {
		return jobdomain.Permanent(err)
	}
	if err != nil {
		return err
	}
	if run.Finished() {
		return nil
	}
	prof, err := s.repo.Profile(ctx, p.WorkspaceID)
	if err != nil {
		return s.finish(ctx, run, visibilitydomain.RunFailed, err.Error())
	}
	st := prof.Settings()
	prompts, err := s.repo.Prompts(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	done, err := s.repo.Answers(ctx, p.WorkspaceID, run.ID)
	if err != nil {
		return err
	}
	made := map[string]bool{}
	for _, a := range done {
		made[a.PromptID.String()+" "+a.Model] = true
	}
	run.Status = visibilitydomain.RunRunning
	if run.StartedAt.IsZero() {
		run.StartedAt = s.now()
	}
	if err := s.repo.SaveRun(ctx, run); err != nil {
		return err
	}
	asker := s.asker(ctx)
	for _, pr := range prompts {
		if !pr.Active {
			continue
		}
		for _, model := range st.Models {
			if made[pr.ID.String()+" "+model] {
				continue
			}
			used, err := s.repo.CallsSince(ctx, p.WorkspaceID, visibilitydomain.MonthStart(s.now()))
			if err != nil {
				return err
			}
			if used >= st.MonthlyCap {
				return s.finish(ctx, run, visibilitydomain.RunCapped, fmt.Sprintf("stopped at this month's cap of %d calls", st.MonthlyCap))
			}
			ans, askErr := asker.Ask(ctx, model, pr.Text)
			if askErr != nil && ctx.Err() != nil {
				return ctx.Err() // cut short: the retry resumes
			}
			a := visibilitydomain.Answer{ID: id.New(), RunID: run.ID, PromptID: pr.ID, Model: model, CreatedAt: s.now()}
			if askErr != nil {
				a.Error = firstN(askErr.Error(), 1000)
				run.Failed++
			} else {
				a.Text, a.Citations = ans.Text, ans.Citations
				a.Analysis = visibilitydomain.Analyze(ans.Text, ans.Citations, st.Brand, st.Competitors)
				run.Done++
				run.Cost += ans.Cost
			}
			if err := s.repo.PutAnswer(ctx, p.WorkspaceID, a); err != nil {
				return err
			}
			if err := s.repo.SaveRun(ctx, run); err != nil {
				return err
			}
		}
	}
	if run.Done == 0 && run.Failed > 0 {
		return s.finish(ctx, run, visibilitydomain.RunFailed, "every call failed; see the answers for why")
	}
	return s.finish(ctx, run, visibilitydomain.RunDone, "")
}

func (s *Service) finish(ctx context.Context, run visibilitydomain.Run, status visibilitydomain.RunStatus, failure string) error {
	run.Status, run.Failure, run.FinishedAt = status, failure, s.now()
	return s.repo.SaveRun(context.WithoutCancel(ctx), run)
}

// ModelStats is how one model's answers treated the brand in a run.
type ModelStats struct {
	Model     string
	Answers   int // answered (failed calls aside)
	Mentioned int
	Cited     int
	// AvgPosition averages the brand's rank where it was named; 0 when
	// it never was.
	AvgPosition float64
}

// Voice is how often a brand was named across a run's answers.
type Voice struct {
	Name     string
	Mentions int
	Cited    int
	Own      bool
}

// Domain is a cited domain and how often.
type Domain struct {
	Domain string
	Count  int
}

// Report is a run read as a whole.
type Report struct {
	Run     visibilitydomain.Run
	Models  []ModelStats
	Voices  []Voice  // the brand first, then competitors by mentions
	Domains []Domain // most cited first
	Answers []visibilitydomain.Answer
}

// Report reads run (the latest finished one when runID is Nil; a zero
// Report when there is none).
func (s *Service) Report(ctx context.Context, ws, runID uuid.UUID) (Report, error) {
	var run visibilitydomain.Run
	if runID == uuid.Nil {
		runs, err := s.repo.Runs(ctx, ws, 20)
		if err != nil {
			return Report{}, err
		}
		i := slices.IndexFunc(runs, func(r visibilitydomain.Run) bool { return r.Finished() && r.Done > 0 })
		if i < 0 {
			return Report{}, nil
		}
		run = runs[i]
	} else {
		var err error
		if run, err = s.repo.Run(ctx, ws, runID); err != nil {
			return Report{}, err
		}
	}
	answers, err := s.repo.Answers(ctx, ws, run.ID)
	if err != nil {
		return Report{}, err
	}
	r := Report{Run: run, Answers: answers}
	var brand string
	if p, err := s.repo.Profile(ctx, ws); err == nil {
		brand = p.Settings().Brand.Name
	}
	models := map[string]*ModelStats{}
	positions := map[string]int{}
	voices := map[string]*Voice{brand: {Name: brand, Own: true}}
	domains := map[string]int{}
	for _, a := range answers {
		m := models[a.Model]
		if m == nil {
			m = &ModelStats{Model: a.Model}
			models[a.Model] = m
		}
		if a.Error != "" {
			continue
		}
		m.Answers++
		if a.Mentioned {
			m.Mentioned++
			positions[a.Model] += a.Position
			voices[brand].Mentions++
		}
		if a.Cited {
			m.Cited++
			voices[brand].Cited++
		}
		for _, c := range a.Competitors {
			if voices[c] == nil {
				voices[c] = &Voice{Name: c}
			}
			voices[c].Mentions++
		}
		for _, c := range a.CompetitorsCited {
			if voices[c] == nil {
				voices[c] = &Voice{Name: c}
			}
			voices[c].Cited++
		}
		seen := map[string]bool{}
		for _, c := range a.Citations {
			if u, err := url.Parse(c); err == nil && u.Hostname() != "" {
				h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
				if !seen[h] {
					seen[h] = true
					domains[h]++
				}
			}
		}
	}
	for _, m := range models {
		if m.Mentioned > 0 {
			m.AvgPosition = float64(positions[m.Model]) / float64(m.Mentioned)
		}
		r.Models = append(r.Models, *m)
	}
	slices.SortFunc(r.Models, func(a, b ModelStats) int { return strings.Compare(a.Model, b.Model) })
	for _, v := range voices {
		r.Voices = append(r.Voices, *v)
	}
	slices.SortFunc(r.Voices, func(a, b Voice) int {
		if a.Own != b.Own {
			if a.Own {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(b.Mentions, a.Mentions), strings.Compare(a.Name, b.Name))
	})
	for d, n := range domains {
		r.Domains = append(r.Domains, Domain{Domain: d, Count: n})
	}
	slices.SortFunc(r.Domains, func(a, b Domain) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Domain, b.Domain))
	})
	if len(r.Domains) > 15 {
		r.Domains = r.Domains[:15]
	}
	return r, nil
}

// Gap is a prompt whose answers in the latest check left the brand out.
type Gap struct {
	Prompt      visibilitydomain.Prompt
	Models      int      // models that answered
	Competitors []string // named instead, most often first
	Citations   []string // sources the answers cited, a few
}

// coverAgain is how soon a prompt a story covered may be offered again.
const coverAgain = 30 * 24 * time.Hour

// Gaps returns up to n active prompts no model named the brand for in
// the latest finished check, not covered by a story in the last 30 days,
// those where competitors were named most first.
func (s *Service) Gaps(ctx context.Context, ws uuid.UUID, n int) ([]Gap, error) {
	rep, err := s.Report(ctx, ws, uuid.Nil)
	if err != nil || rep.Run.ID == uuid.Nil {
		return nil, err
	}
	prompts, err := s.repo.Prompts(ctx, ws)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]visibilitydomain.Prompt{}
	for _, p := range prompts {
		byID[p.ID] = p
	}
	type acc struct {
		answered, mentioned int
		competitors         map[string]int
		citations           []string
	}
	per := map[uuid.UUID]*acc{}
	for _, a := range rep.Answers {
		if a.Error != "" {
			continue
		}
		x := per[a.PromptID]
		if x == nil {
			x = &acc{competitors: map[string]int{}}
			per[a.PromptID] = x
		}
		x.answered++
		if a.Mentioned {
			x.mentioned++
		}
		for _, c := range a.Competitors {
			x.competitors[c]++
		}
		for _, c := range a.Citations {
			if len(x.citations) < 5 && !slices.Contains(x.citations, c) {
				x.citations = append(x.citations, c)
			}
		}
	}
	now := s.now()
	var out []Gap
	for pid, x := range per {
		p, ok := byID[pid]
		if !ok || !p.Active || x.answered == 0 || x.mentioned > 0 || (p.CoveredBy != uuid.Nil && now.Sub(p.CoveredAt) < coverAgain) {
			continue
		}
		g := Gap{Prompt: p, Models: x.answered, Citations: x.citations}
		for c := range x.competitors {
			g.Competitors = append(g.Competitors, c)
		}
		slices.SortFunc(g.Competitors, func(a, b string) int {
			return cmp.Or(cmp.Compare(x.competitors[b], x.competitors[a]), strings.Compare(a, b))
		})
		out = append(out, g)
	}
	slices.SortFunc(out, func(a, b Gap) int {
		return cmp.Or(cmp.Compare(len(b.Competitors), len(a.Competitors)), a.Prompt.CreatedAt.Compare(b.Prompt.CreatedAt))
	})
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// Cover records that story sets out to answer prompt, so the next
// pipeline run picks another.
func (s *Service) Cover(ctx context.Context, ws, promptID, storyID uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.repo.Prompt(ctx, ws, promptID)
		if err != nil {
			return err
		}
		p.CoveredBy, p.CoveredAt, p.UpdatedAt = storyID, s.now(), s.now()
		return s.repo.PutPrompt(ctx, p)
	})
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
