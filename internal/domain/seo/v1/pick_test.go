package seo_test

import (
	"testing"

	seodomain "kurgu/internal/domain/seo/v1"
)

func TestPick(t *testing.T) {
	cands := []seodomain.Candidate{
		{Keyword: "kaygı bozukluğu belirtileri", Volume: 5400, Difficulty: 28, Intent: "informational"},
		{Keyword: "acme giriş", Volume: 900, Difficulty: 5, Intent: "navigational"},                   // brand
		{Keyword: "Kaygı  Bozukluğu belirtileri", Volume: 5400, Difficulty: 28, Intent: "informational"}, // duplicate
		{Keyword: "belirtileri kaygı bozukluğu", Volume: 100, Difficulty: 10, Intent: "informational"},   // same words
		{Keyword: "terapist fiyatları", Volume: 9900, Difficulty: 20, Intent: "transactional"},
		{Keyword: "panik atak nedir", Volume: 12000, Difficulty: 72, Intent: "informational"}, // too hard
		{Keyword: "uyku hijyeni", Volume: 5, Difficulty: 3, Intent: "informational"},          // too rare
		{Keyword: "ruminasyon nedir", Volume: 1600, Difficulty: 12, Intent: "informational"},
		{Keyword: "endişe ile kaygı farkı", Volume: -1, Difficulty: -1, Intent: "informational"},
		{Keyword: "online terapi", Volume: 2900, Difficulty: 40, Intent: "commercial"},
	}
	got := seodomain.Pick(cands, []string{"Ruminasyon Nedir"}, "acme", nil, 3, 50, 10)
	want := []string{"kaygı bozukluğu belirtileri", "online terapi", "terapist fiyatları"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		if got[i].Keyword != w {
			t.Fatalf("rank %d = %q, want %q (all: %+v)", i, got[i].Keyword, w, got)
		}
	}
	// Unknown metrics are kept when no volume floor is set.
	got = seodomain.Pick(cands, nil, "", nil, 20, 50, 0)
	found := false
	for _, c := range got {
		found = found || c.Keyword == "endişe ile kaygı farkı"
	}
	if !found {
		t.Fatalf("unknown-metric keyword dropped: %+v", got)
	}
}

func TestProfileSeeds(t *testing.T) {
	p := seodomain.Profile{Topics: "kaygı, depresyon\nuyku; kaygı, a, b, c, d"}
	if s := p.SeedList(); len(s) != 5 || s[0] != "kaygı" || s[2] != "uyku" {
		t.Fatalf("seeds = %q", s)
	}
	p.Seeds, p.Competitors = []string{"terapi"}, []string{"example.com"}
	if p.EstimatedCredits() != 54+300 || p.Difficulty() != 50 {
		t.Fatalf("estimate = %d, difficulty = %d", p.EstimatedCredits(), p.Difficulty())
	}
}

// Live research for the seed "kaygı" returned popular health queries;
// seeded ideas must share a stem with the seeds or topics.
func TestPickKeepsSeededIdeasOnTopic(t *testing.T) {
	cands := []seodomain.Candidate{
		{Keyword: "kortizol nedir", Volume: 90500, Difficulty: 0, Intent: "informational", Seeded: true},
		{Keyword: "astigmat nedir", Volume: 60500, Difficulty: 0, Intent: "informational", Seeded: true},
		{Keyword: "kaygı bozukluğu belirtileri", Volume: 5400, Difficulty: 10, Intent: "informational", Seeded: true},
		{Keyword: "kaygının fiziksel belirtileri", Volume: 900, Difficulty: 5, Intent: "informational", Seeded: true},
		{Keyword: "online terapist", Volume: 2400, Difficulty: 15, Intent: "commercial", Seeded: true},
		{Keyword: "İLİŞKİ terapisi", Volume: 1000, Difficulty: 15, Intent: "informational", Seeded: true},
		{Keyword: "uyku hijyeni nedir", Volume: 3000, Difficulty: 5, Intent: "informational"}, // from a competitor
	}
	got := seodomain.Pick(cands, nil, "", []string{"kaygı", "terapi", "ilişki sorunları nedir"}, 10, 50, 0)
	var names []string
	for _, c := range got {
		names = append(names, c.Keyword)
	}
	want := map[string]bool{"kaygı bozukluğu belirtileri": true, "kaygının fiziksel belirtileri": true, "online terapist": true,
		"İLİŞKİ terapisi": true, "uyku hijyeni nedir": true}
	if len(got) != len(want) {
		t.Fatalf("picked %q", names)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("picked off-topic %q (all: %q)", n, names)
		}
	}
	if s := seodomain.Stems([]string{"Kaygı, terapi", "nedir"}); len(s) != 2 || s[0] != "kayg" || s[1] != "terap" {
		t.Fatalf("stems = %q", s)
	}
}
