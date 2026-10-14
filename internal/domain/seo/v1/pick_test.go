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
	got := seodomain.Pick(cands, []string{"Ruminasyon Nedir"}, "acme", 3, 50, 10)
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
	got = seodomain.Pick(cands, nil, "", 20, 50, 0)
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
