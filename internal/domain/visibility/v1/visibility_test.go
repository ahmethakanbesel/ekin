package visibility_test

import (
	"slices"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	visibility "github.com/ahmethakanbesel/ekin/internal/domain/visibility/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

var now = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

func settings() visibility.Settings {
	return visibility.Settings{
		Brand:       visibility.Brand{Name: " Acme ", Aliases: []string{"Acme Tools", "acme"}, Domain: "https://www.acme.com/"},
		Competitors: []visibility.Brand{{Name: "Globex", Domain: "globex.io"}, {Name: "Initech"}},
		Models:      []string{"Perplexity/Sonar", "openai/gpt-5-mini:online", "perplexity/sonar"},
	}
}

func TestProfile(t *testing.T) {
	p, err := visibility.NewProfile(id.New(), settings(), now)
	if err != nil {
		t.Fatal(err)
	}
	st := p.Settings()
	if st.Brand.Name != "Acme" || st.Brand.Domain != "acme.com" || len(st.Brand.Aliases) != 1 || len(st.Models) != 2 ||
		st.Schedule != visibility.ScheduleWeekly || st.MonthlyCap != visibility.DefaultCap {
		t.Fatalf("settings = %+v", st)
	}
	if p.Due(now.Add(6*24*time.Hour)) || !p.Due(now.Add(7*24*time.Hour)) {
		t.Fatal("weekly schedule")
	}
	bad := []func(*visibility.Settings){
		func(s *visibility.Settings) { s.Brand.Name = "" },
		func(s *visibility.Settings) { s.Models = nil },
		func(s *visibility.Settings) { s.Models = []string{"gpt"} },
		func(s *visibility.Settings) { s.Competitors[0].Domain = "not a domain" },
		func(s *visibility.Settings) { s.MonthlyCap = -1 },
		func(s *visibility.Settings) { s.Schedule = "hourly" },
	}
	for i, mut := range bad {
		s := settings()
		mut(&s)
		if err := p.Update(s, now); !apperror.Is(err, apperror.ValidationFailed) {
			t.Errorf("case %d accepted: %v", i, err)
		}
	}
	back, err := visibility.RehydrateProfile(p.Snapshot())
	if err != nil || back.Settings().Brand.Name != "Acme" {
		t.Fatalf("rehydrate = %v", err)
	}
}

func TestAllow(t *testing.T) {
	if err := visibility.Allow(30, 470, 500); err != nil {
		t.Fatalf("a run that fits: %v", err)
	}
	if err := visibility.Allow(31, 470, 500); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("a run past the cap: %v", err)
	}
	if err := visibility.Allow(0, 0, 500); err == nil {
		t.Fatal("an empty run")
	}
	if got := visibility.MonthStart(now); !got.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("month start = %v", got)
	}
}

func TestAnalyze(t *testing.T) {
	st := settings()
	p, _ := visibility.NewProfile(id.New(), st, now)
	st = p.Settings()
	cases := []struct {
		name      string
		text      string
		citations []string
		want      visibility.Analysis
	}{
		{"named second, cited", "The best picks: Globex, then Acme Tools for small teams.", []string{"https://docs.acme.com/x", "https://globex.io/"},
			visibility.Analysis{Mentioned: true, Position: 2, Cited: true, Competitors: []string{"Globex"}, CompetitorsCited: []string{"Globex"}}},
		{"named by domain first", "Try acme.com. Initech also works.", nil,
			visibility.Analysis{Mentioned: true, Position: 1, Competitors: []string{"Initech"}}},
		{"absent; a word containing the name does not count", "Acmeology and Globex are popular.", []string{"https://notacme.com"},
			visibility.Analysis{Competitors: []string{"Globex"}}},
		{"any case, punctuation around", "(ACME) is fine.", nil, visibility.Analysis{Mentioned: true, Position: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := visibility.Analyze(c.text, c.citations, st.Brand, st.Competitors)
			if got.Mentioned != c.want.Mentioned || got.Position != c.want.Position || got.Cited != c.want.Cited ||
				!slices.Equal(got.Competitors, c.want.Competitors) || !slices.Equal(got.CompetitorsCited, c.want.CompetitorsCited) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
