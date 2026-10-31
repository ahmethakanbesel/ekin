package visibility_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/adapter/claude"
	"github.com/ahmethakanbesel/ekin/internal/adapter/openrouter"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	visibilitydomain "github.com/ahmethakanbesel/ekin/internal/domain/visibility/v1"
	visibilityrepo "github.com/ahmethakanbesel/ekin/internal/repository/visibility/v1"
	visibilitysvc "github.com/ahmethakanbesel/ekin/internal/service/visibility/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// fakeAsker answers per model: sonar names Acme second after Globex and
// cites acme.com; mini names only Globex; broken/x fails.
type fakeAsker struct {
	mu     sync.Mutex
	calls  int
	stop   func() // runs before the call numbered stopAt
	stopAt int
}

func (*fakeAsker) Enabled() bool { return true }
func (f *fakeAsker) Ask(ctx context.Context, model, prompt string) (openrouter.Answer, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	if f.stop != nil && n == f.stopAt {
		f.stop()
		return openrouter.Answer{}, ctx.Err()
	}
	switch model {
	case "perplexity/sonar":
		if strings.Contains(prompt, "pricing") {
			return openrouter.Answer{Text: "Globex is cheapest; Initech too.", Citations: []string{"https://review.example/pricing"}, Cost: 0.001}, nil
		}
		return openrouter.Answer{Text: "Globex leads, then Acme for small teams.", Citations: []string{"https://acme.com/", "https://review.example/x"}, Cost: 0.002}, nil
	case "openai/mini":
		return openrouter.Answer{Text: "Most pick Globex."}, nil
	}
	return openrouter.Answer{}, errors.New("unknown model")
}

type fakeAgent struct{ prompts []string }

func (a *fakeAgent) Run(_ context.Context, req claude.Request) (claude.Result, error) {
	b, _ := json.Marshal(map[string]any{"prompts": a.prompts})
	return claude.Result{Structured: b}, nil
}

type jobs struct{ specs []jobdomain.Spec }

func (j *jobs) Enqueue(_ context.Context, s jobdomain.Spec) (*jobdomain.Job, bool, error) {
	j.specs = append(j.specs, s)
	return nil, true, nil
}

type fixture struct {
	svc   *visibilitysvc.Service
	asker *fakeAsker
	agent *fakeAgent
	jobs  *jobs
	ws    uuid.UUID
	clock *time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	clock := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	f := fixture{asker: &fakeAsker{}, agent: &fakeAgent{}, jobs: &jobs{}, ws: id.New(), clock: &clock}
	f.svc = visibilitysvc.NewService(visibilitysvc.Deps{
		Repo: visibilityrepo.NewInMemory(), Tx: &db.MemTxManager{}, Jobs: f.jobs, Agent: f.agent,
		Asker: func(context.Context) visibilitysvc.Asker { return f.asker },
		Known: func(context.Context, uuid.UUID) visibilitysvc.Context {
			return visibilitysvc.Context{Domain: "https://acme-tools.com", Competitors: []string{"globex.io", "acme-tools.com"}, Topics: "team tools"}
		},
	}).WithClock(func() time.Time { return *f.clock })
	return f
}

func (f fixture) runJobs(t *testing.T) {
	t.Helper()
	for len(f.jobs.specs) > 0 {
		s := f.jobs.specs[0]
		f.jobs.specs = f.jobs.specs[1:]
		var err error
		switch s.Kind {
		case visibilitysvc.KindRun:
			var p visibilitysvc.RunPayload
			_ = json.Unmarshal(s.Payload, &p)
			err = f.svc.RunCheck(context.Background(), p)
		case visibilitysvc.KindDraft:
			var p visibilitysvc.DraftPayload
			_ = json.Unmarshal(s.Payload, &p)
			err = f.svc.RunDraft(context.Background(), p)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func setUp(t *testing.T, f fixture, models []string, monthlyCap int) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.svc.SaveSettings(ctx, f.ws, visibilitydomain.Settings{
		Brand: visibilitydomain.Brand{Name: "Acme", Domain: "acme.com"}, Competitors: []visibilitydomain.Brand{{Name: "Globex"}, {Name: "Initech"}},
		Models: models, MonthlyCap: monthlyCap,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSuggestDraftAndRun(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	o, err := f.svc.Overview(ctx, f.ws)
	if err != nil || o.Profile != nil || o.Suggested.Brand.Name != "Acme Tools" || len(o.Suggested.Competitors) != 1 || o.Suggested.Competitors[0].Name != "Globex" {
		t.Fatalf("suggested = %+v %v", o.Suggested, err)
	}
	local := visibilitysvc.NewService(visibilitysvc.Deps{Repo: visibilityrepo.NewInMemory(), Tx: &db.MemTxManager{}, Jobs: f.jobs,
		Asker: func(context.Context) visibilitysvc.Asker { return f.asker },
		Known: func(context.Context, uuid.UUID) visibilitysvc.Context {
			return visibilitysvc.Context{Domain: "http://127.0.0.1:8091/"}
		}})
	if o, _ := local.Overview(ctx, f.ws); o.Suggested.Brand.Name != "" || o.Suggested.Brand.Domain != "" {
		t.Fatalf("an IP address suggested a brand: %+v", o.Suggested.Brand)
	}
	if _, err := f.svc.QueueDraft(ctx, f.ws, 5); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("drafted before setting up: %v", err)
	}
	setUp(t, f, []string{"perplexity/sonar", "openai/mini"}, 100)
	if _, err := f.svc.AddPrompt(ctx, f.ws, "best tools for small teams"); err != nil {
		t.Fatal(err)
	}
	f.agent.prompts = []string{"Best Tools For Small Teams", "cheapest team tool pricing", "  "}
	if _, err := f.svc.QueueDraft(ctx, f.ws, 5); err != nil {
		t.Fatal(err)
	}
	f.runJobs(t)
	o, _ = f.svc.Overview(ctx, f.ws)
	if len(o.Prompts) != 2 || o.Prompts[1].Source != visibilitydomain.SourceAgent || o.Estimate.Calls != 4 || o.Estimate.Cap != 100 {
		t.Fatalf("prompts %+v, estimate %+v", o.Prompts, o.Estimate)
	}

	run, err := f.svc.StartRun(ctx, f.ws, "manual")
	if err != nil || run.Planned != 4 {
		t.Fatalf("start = %+v %v", run, err)
	}
	if _, err := f.svc.StartRun(ctx, f.ws, "manual"); !apperror.Is(err, apperror.Conflict) {
		t.Fatalf("a second run while one is queued: %v", err)
	}
	f.runJobs(t)
	rep, err := f.svc.Report(ctx, f.ws, uuid.Nil)
	if err != nil || rep.Run.Status != visibilitydomain.RunDone || rep.Run.Done != 4 || len(rep.Answers) != 4 {
		t.Fatalf("report = %+v %v", rep.Run, err)
	}
	sonar := rep.Models[1]
	if sonar.Model != "perplexity/sonar" || sonar.Mentioned != 1 || sonar.Cited != 1 || sonar.AvgPosition != 2 {
		t.Fatalf("sonar = %+v", sonar)
	}
	if rep.Voices[0].Name != "Acme" || rep.Voices[0].Mentions != 1 || rep.Voices[1].Name != "Globex" || rep.Voices[1].Mentions != 4 {
		t.Fatalf("voices = %+v", rep.Voices)
	}
	if rep.Domains[0].Domain != "review.example" || rep.Domains[0].Count != 2 {
		t.Fatalf("domains = %+v", rep.Domains)
	}

	// The pricing prompt left Acme out everywhere: a gap, until covered.
	gaps, err := f.svc.Gaps(ctx, f.ws, 5)
	if err != nil || len(gaps) != 1 || !strings.Contains(gaps[0].Prompt.Text, "pricing") || gaps[0].Competitors[0] != "Globex" || len(gaps[0].Citations) != 1 {
		t.Fatalf("gaps = %+v %v", gaps, err)
	}
	if err := f.svc.Cover(ctx, f.ws, gaps[0].Prompt.ID, id.New()); err != nil {
		t.Fatal(err)
	}
	if gaps, _ := f.svc.Gaps(ctx, f.ws, 5); len(gaps) != 0 {
		t.Fatalf("a covered prompt is offered again: %+v", gaps)
	}
}

func TestCapAndResume(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	setUp(t, f, []string{"perplexity/sonar", "openai/mini", "broken/x"}, 5)
	for _, p := range []string{"one", "two"} {
		if _, err := f.svc.AddPrompt(ctx, f.ws, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.StartRun(ctx, f.ws, "manual"); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("a 6-call run under a cap of 5: %v", err)
	}
	setUp(t, f, []string{"perplexity/sonar", "openai/mini", "broken/x"}, 9)

	// The worker stops on the third call; the retry resumes after the two made.
	runCtx, cancel := context.WithCancel(ctx)
	f.asker.stop, f.asker.stopAt = cancel, 3
	run, _ := f.svc.StartRun(ctx, f.ws, "manual")
	payload := visibilitysvc.RunPayload{WorkspaceID: f.ws, RunID: run.ID}
	f.jobs.specs = nil
	if err := f.svc.RunCheck(runCtx, payload); !errors.Is(err, context.Canceled) {
		t.Fatalf("cut short = %v", err)
	}
	f.asker.stop = nil
	if err := f.svc.RunCheck(ctx, payload); err != nil {
		t.Fatal(err)
	}
	rep, _ := f.svc.Report(ctx, f.ws, run.ID)
	if len(rep.Answers) != 6 || rep.Run.Done != 4 || rep.Run.Failed != 2 || rep.Run.Status != visibilitydomain.RunDone {
		t.Fatalf("resumed run = %+v, %d answers", rep.Run, len(rep.Answers))
	}

	// 6 of 9 used: the next 6-call run is refused, and a run already
	// going stops at the cap.
	if _, err := f.svc.StartRun(ctx, f.ws, "manual"); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("over the month's cap: %v", err)
	}
	*f.clock = f.clock.Add(48 * time.Hour) // November: a fresh month
	run, err := f.svc.StartRun(ctx, f.ws, "manual")
	if err != nil {
		t.Fatal(err)
	}
	setUp(t, f, []string{"perplexity/sonar", "openai/mini", "broken/x"}, 4)
	f.runJobs(t)
	rep, _ = f.svc.Report(ctx, f.ws, run.ID)
	if rep.Run.Status != visibilitydomain.RunCapped || len(rep.Answers) != 4 {
		t.Fatalf("capped run = %+v, %d answers", rep.Run, len(rep.Answers))
	}
}

func TestScheduleAndRetention(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	setUp(t, f, []string{"perplexity/sonar"}, 100)
	_, _ = f.svc.AddPrompt(ctx, f.ws, "best tools")
	if n, _ := f.svc.TriggerDue(ctx); n != 0 {
		t.Fatalf("ran before it was due: %d", n)
	}
	*f.clock = f.clock.Add(7 * 24 * time.Hour)
	if n, _ := f.svc.TriggerDue(ctx); n != 1 {
		t.Fatalf("weekly run = %d", n)
	}
	f.runJobs(t)
	*f.clock = f.clock.Add(visibilitydomain.AnswerRetention + time.Hour)
	_, _ = f.svc.TriggerDue(ctx)
	rep, _ := f.svc.Report(ctx, f.ws, uuid.Nil)
	if len(rep.Answers) != 1 || rep.Answers[0].Text != "" || !rep.Answers[0].Mentioned {
		t.Fatalf("after retention: %+v", rep.Answers)
	}
}
