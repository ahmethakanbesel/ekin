package visibility_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db/dbtest"
	visibilitydomain "github.com/ahmethakanbesel/ekin/internal/domain/visibility/v1"
	workspacedomain "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	visibilityrepo "github.com/ahmethakanbesel/ekin/internal/repository/visibility/v1"
	workspacerepo "github.com/ahmethakanbesel/ekin/internal/repository/workspace/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

var now = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

func impls() map[string]func(t *testing.T) (visibilityrepo.Repository, uuid.UUID, uuid.UUID) {
	return map[string]func(t *testing.T) (visibilityrepo.Repository, uuid.UUID, uuid.UUID){
		"inmemory": func(*testing.T) (visibilityrepo.Repository, uuid.UUID, uuid.UUID) {
			return visibilityrepo.NewInMemory(), id.New(), id.New()
		},
		"sqlite": func(t *testing.T) (visibilityrepo.Repository, uuid.UUID, uuid.UUID) {
			_, tx := dbtest.Open(t)
			var ids []uuid.UUID
			for _, name := range []string{"a", "b"} {
				w, _ := workspacedomain.New(id.New(), name, time.UTC, now)
				if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, w.ID())
			}
			return visibilityrepo.NewSQLite(tx), ids[0], ids[1]
		},
	}
}

func TestVisibilityRepository(t *testing.T) {
	for name, open := range impls() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws, other := open(t)
			if _, err := repo.Profile(ctx, ws); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("no profile = %v", err)
			}
			p, err := visibilitydomain.NewProfile(ws, visibilitydomain.Settings{
				Brand: visibilitydomain.Brand{Name: "Acme", Domain: "acme.com"}, Competitors: []visibilitydomain.Brand{{Name: "Globex"}},
				Models: []string{"perplexity/sonar"}, Schedule: visibilitydomain.ScheduleDaily, MonthlyCap: 100,
			}, now)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.PutProfile(ctx, p); err != nil {
				t.Fatal(err)
			}
			got, err := repo.Profile(ctx, ws)
			if err != nil || got.Settings().Brand.Domain != "acme.com" || got.Settings().MonthlyCap != 100 || len(got.Settings().Competitors) != 1 {
				t.Fatalf("profile = %+v %v", got, err)
			}
			if due, _ := repo.DueProfiles(ctx, now.Add(25*time.Hour)); len(due) != 1 {
				t.Fatalf("due = %d", len(due))
			}

			first := visibilitydomain.Prompt{ID: id.New(), WorkspaceID: ws, Text: "best tools", Source: visibilitydomain.SourceAgent, Active: true, CreatedAt: now, UpdatedAt: now}
			second := visibilitydomain.Prompt{ID: id.New(), WorkspaceID: ws, Text: "acme vs globex", Source: visibilitydomain.SourcePerson, Active: true, CreatedAt: now.Add(time.Second), UpdatedAt: now}
			for _, x := range []visibilitydomain.Prompt{first, second} {
				if err := repo.PutPrompt(ctx, x); err != nil {
					t.Fatal(err)
				}
			}
			second.Active, second.CoveredBy, second.CoveredAt = false, id.New(), now
			_ = repo.PutPrompt(ctx, second)
			list, _ := repo.Prompts(ctx, ws)
			if len(list) != 2 || list[0].ID != first.ID || list[1].Active || list[1].CoveredBy != second.CoveredBy {
				t.Fatalf("prompts = %+v", list)
			}
			if _, err := repo.Prompt(ctx, other, first.ID); !apperror.Is(err, apperror.NotFound) {
				t.Fatalf("another workspace's prompt = %v", err)
			}

			run := visibilitydomain.Run{ID: id.New(), WorkspaceID: ws, Status: visibilitydomain.RunQueued, Trigger: "manual", Planned: 2, CreatedAt: now}
			if err := repo.CreateRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			run.Status, run.Done, run.Cost, run.StartedAt, run.FinishedAt = visibilitydomain.RunDone, 2, 0.01, now, now.Add(time.Minute)
			_ = repo.SaveRun(ctx, run)
			if got, err := repo.Run(ctx, ws, run.ID); err != nil || got.Status != visibilitydomain.RunDone || got.Done != 2 || !got.FinishedAt.Equal(now.Add(time.Minute)) {
				t.Fatalf("run = %+v %v", got, err)
			}
			old := now.Add(-100 * 24 * time.Hour)
			answers := []visibilitydomain.Answer{
				{ID: id.New(), RunID: run.ID, PromptID: second.ID, Model: "perplexity/sonar", Text: "Acme wins", Citations: []string{"https://acme.com"},
					Analysis: visibilitydomain.Analysis{Mentioned: true, Position: 1, Cited: true, Competitors: []string{"Globex"}}, CreatedAt: now},
				{ID: id.New(), RunID: run.ID, PromptID: first.ID, Model: "perplexity/sonar", Text: "Globex", CreatedAt: old},
			}
			for _, a := range answers {
				if err := repo.PutAnswer(ctx, ws, a); err != nil {
					t.Fatal(err)
				}
			}
			got2, err := repo.Answers(ctx, ws, run.ID)
			if err != nil || len(got2) != 2 || got2[0].PromptID != first.ID || !got2[1].Mentioned || got2[1].Competitors[0] != "Globex" || got2[1].Citations[0] != "https://acme.com" {
				t.Fatalf("answers = %+v %v", got2, err)
			}
			if n, _ := repo.CallsSince(ctx, ws, visibilitydomain.MonthStart(now)); n != 1 {
				t.Fatalf("calls this month = %d", n)
			}
			if n, _ := repo.ForgetTexts(ctx, now.Add(-visibilitydomain.AnswerRetention)); n != 1 {
				t.Fatalf("forgot %d", n)
			}
			got2, _ = repo.Answers(ctx, ws, run.ID)
			if got2[0].Text != "" || got2[1].Text == "" {
				t.Fatalf("texts after forgetting = %q %q", got2[0].Text, got2[1].Text)
			}
			if err := repo.DeletePrompt(ctx, ws, first.ID); err != nil {
				t.Fatal(err)
			}
			if left, _ := repo.Answers(ctx, ws, run.ID); len(left) != 1 {
				t.Fatalf("a deleted prompt's answers remain: %d", len(left))
			}
		})
	}
}
