package seo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/adapter/claude"
	"github.com/ahmethakanbesel/ekin/internal/adapter/openseo"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db/dbtest"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	seodomain "github.com/ahmethakanbesel/ekin/internal/domain/seo/v1"
	workspacedomain "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	jobrepo "github.com/ahmethakanbesel/ekin/internal/repository/job/v1"
	seorepo "github.com/ahmethakanbesel/ekin/internal/repository/seo/v1"
	settingsrepo "github.com/ahmethakanbesel/ekin/internal/repository/settings/v1"
	workspacerepo "github.com/ahmethakanbesel/ekin/internal/repository/workspace/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	seosvc "github.com/ahmethakanbesel/ekin/internal/service/seo/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

type noAgent struct{ called bool }

func (a *noAgent) Run(context.Context, claude.Request) (claude.Result, error) {
	a.called = true
	return claude.Result{}, errors.New("the agent must not run when an API key is set")
}

type fakeResearch struct {
	seeds   []string
	market  openseo.Market
	domains []string
}

func (f *fakeResearch) Whoami(context.Context) (openseo.Account, error) {
	c := 900.0
	return openseo.Account{UserEmail: "a@example.com", CreditsRemaining: &c}, nil
}
func (f *fakeResearch) Projects(context.Context) ([]openseo.Project, error) {
	return []openseo.Project{{ID: "p1", Name: "acme", Domain: "example.com", LocationCode: 2792, LanguageCode: "tr"}}, nil
}
func (f *fakeResearch) ResearchKeywords(_ context.Context, projectID string, seeds []string, m openseo.Market) ([]openseo.SeedResult, error) {
	f.seeds, f.market = seeds, m
	n := func(v int) *int { return &v }
	return []openseo.SeedResult{
		{Seed: seeds[0], OK: true, Rows: []openseo.Keyword{
			{Keyword: "kaygı bozukluğu belirtileri", SearchVolume: n(5400), KeywordDifficulty: n(28), Intent: "informational"},
			{Keyword: "acme randevu", SearchVolume: n(300), KeywordDifficulty: n(2), Intent: "navigational"},
			{Keyword: "kaygı nedir", SearchVolume: n(9000), KeywordDifficulty: n(30), Intent: "informational"}, // already in the pool
			{Keyword: "panik atak tedavisi", SearchVolume: n(4000), KeywordDifficulty: n(80), Intent: "informational"},
		}},
		{Seed: "x", OK: false, Error: "no data"},
	}, nil
}
func (f *fakeResearch) DomainKeywords(_ context.Context, _, domain string, _ openseo.Market) ([]openseo.Keyword, error) {
	f.domains = append(f.domains, domain)
	n := func(v int) *int { return &v }
	return []openseo.Keyword{{Keyword: "ruminasyon nedir", SearchVolume: n(1600), KeywordDifficulty: n(12), Intent: "informational"}}, nil
}

func TestDirectRefresh(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, time.Now())
	_ = workspacerepo.NewSQLite(tx).Create(ctx, w)
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	agent := &noAgent{}
	research := &fakeResearch{}
	var key bool
	svc := seosvc.NewService(seorepo.NewSQLite(tx), settingsrepo.NewSQLite(tx), jobs, agent, t.TempDir()).
		WithResearch(func(context.Context) seosvc.Research {
			if !key {
				return nil
			}
			return research
		})

	if st := svc.OpenSEO(ctx); st.Configured {
		t.Fatalf("configured without a key: %+v", st)
	}
	key = true
	if st := svc.OpenSEO(ctx); !st.Configured || st.Account.UserEmail != "a@example.com" || len(st.Projects) != 1 {
		t.Fatalf("status = %+v", st)
	}

	prof := seodomain.Profile{Domain: "https://example.com/", Country: "tr", Language: "tr", Topics: "kaygı, x",
		Competitors: []string{"https://example.com/", "example.com"}, MaxDifficulty: 50}
	if _, err := svc.SaveProfile(ctx, w.ID(), id.New(), prof); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.QueueRefresh(ctx, w.ID(), 10); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("refresh without a project: %v", err)
	}
	prof.ProjectID = "p1"
	saved, err := svc.SaveProfile(ctx, w.ID(), id.New(), prof)
	if err != nil || len(saved.Competitors) != 1 || saved.EstimatedCredits() != 2*54+300 {
		t.Fatalf("saved = %+v %v", saved, err)
	}
	if _, err := svc.Import(ctx, w.ID(), []seosvc.Item{{Keyword: "Kaygı nedir", Language: "tr", Difficulty: -1}}, "manual"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.QueueRefresh(ctx, w.ID(), 10); err != nil {
		t.Fatal(err)
	}
	j, _ := jobs.Claim(ctx, []jobdomain.Queue{jobdomain.QueueAgent}, []string{seosvc.KindRefresh}, "t", time.Minute)
	var p seosvc.RefreshPayload
	_ = json.Unmarshal(j.Payload(), &p)
	if err := svc.Refresh(ctx, p); err != nil {
		t.Fatal(err)
	}
	if agent.called {
		t.Fatal("the agent ran")
	}
	if strings.Join(research.seeds, ",") != "kaygı,x" || research.market.LocationCode != 2792 || research.market.LanguageCode != "tr" ||
		strings.Join(research.domains, ",") != "example.com" {
		t.Fatalf("calls: seeds %v market %+v domains %v", research.seeds, research.market, research.domains)
	}
	open, _, _ := svc.List(ctx, w.ID(), seosvc.Filter{Statuses: []seodomain.Status{seodomain.StatusOpen}})
	var got []string
	for _, o := range open {
		if o.Source == "openseo" {
			got = append(got, o.Keyword)
			if o.Keyword == "ruminasyon nedir" && (o.Note != "From example.com · 1600 searches/month · difficulty 12" || o.Difficulty != 12) {
				t.Fatalf("opportunity = %+v", o)
			}
		}
	}
	if len(got) != 2 || !strings.Contains(strings.Join(got, ","), "kaygı bozukluğu belirtileri") || !strings.Contains(strings.Join(got, ","), "ruminasyon nedir") {
		t.Fatalf("imported %v (brand, known and too-hard keywords must be skipped)", got)
	}
}

// The best open keyword is found however many newer ones came after it
// (the pool was cut at the newest 1000 and then ranked).
func TestNextFindsTheBestOfAPoolOfAnySize(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, time.Now())
	_ = workspacerepo.NewSQLite(tx).Create(ctx, w)
	svc := seosvc.NewService(seorepo.NewSQLite(tx), settingsrepo.NewSQLite(tx), nil, &noAgent{}, t.TempDir())
	if _, err := svc.Import(ctx, w.ID(), []seosvc.Item{{Keyword: "kaygı nedir", Language: "tr", Volume: 9000, Difficulty: 10}}, "manual"); err != nil {
		t.Fatal(err)
	}
	// 1200 newer, worse keywords.
	for batch := range 3 {
		items := make([]seosvc.Item, 0, 400)
		for i := range 400 {
			items = append(items, seosvc.Item{Keyword: fmt.Sprintf("uzun kuyruk %d %d", batch, i), Language: "tr", Volume: 20, Difficulty: 40})
		}
		if n, err := svc.Import(ctx, w.ID(), items, "manual"); err != nil || n != 400 {
			t.Fatalf("import = %d, %v", n, err)
		}
	}
	next, err := svc.Next(ctx, w.ID(), "tr", 1)
	if err != nil || len(next) != 1 || next[0].Keyword != "kaygı nedir" {
		t.Fatalf("next = %+v, %v", next, err)
	}
}
