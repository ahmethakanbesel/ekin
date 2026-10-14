// Package seo models keyword opportunities: search terms worth writing
// for, collected from OpenSEO (or pasted in) and consumed one per story
// by pipelines whose source is "seo".
package seo

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	storydomain "kurgu/internal/domain/story/v1"
	"kurgu/pkg/timeutil"
)

// Status is an opportunity's state in the pool.
type Status string

const (
	StatusOpen      Status = "open"
	StatusUsed      Status = "used"
	StatusDismissed Status = "dismissed"
)

// Opportunity is one keyword to target.
type Opportunity struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Keyword     string
	Language    string // tr | en
	Volume      int    // monthly searches, 0 if unknown
	Difficulty  int    // 0–100, -1 if unknown
	Intent      string // informational, commercial, ...
	Note        string // why it is an opportunity
	Source      string // openseo | manual
	Status      Status
	StoryID     uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// New checks a candidate opportunity.
func New(id, ws uuid.UUID, keyword, language string, volume, difficulty int, intent, note, source string, now time.Time) (Opportunity, error) {
	keyword = strings.Join(strings.Fields(keyword), " ")
	if keyword == "" || utf8.RuneCountInString(keyword) > 120 {
		return Opportunity{}, apperror.New(apperror.ValidationFailed, "keyword must be 1 to 120 characters")
	}
	if language != "tr" && language != "en" {
		return Opportunity{}, apperror.New(apperror.ValidationFailed, "language must be tr or en")
	}
	if volume < 0 {
		volume = 0
	}
	if difficulty > 100 {
		difficulty = 100
	}
	if difficulty < -1 {
		difficulty = -1
	}
	if len(note) > 1000 {
		note = note[:1000]
	}
	now = timeutil.Normalize(now)
	return Opportunity{ID: id, WorkspaceID: ws, Keyword: keyword, Language: language, Volume: volume, Difficulty: difficulty,
		Intent: strings.ToLower(strings.TrimSpace(intent)), Note: strings.TrimSpace(note), Source: source,
		Status: StatusOpen, CreatedAt: now, UpdatedAt: now}, nil
}

// Norm is the keyword folded for duplicate detection: case, Turkish
// dotted and dotless i, accents and punctuation do not matter.
func (o Opportunity) Norm() string { return storydomain.Fingerprint(o.Keyword) }

// Key identifies the keyword within a workspace.
func (o Opportunity) Key() string { return o.Norm() + "|" + o.Language }

// Score ranks open opportunities: demand discounted by difficulty, with
// informational intent (what educational content wins) preferred.
func (o Opportunity) Score() float64 {
	volume := float64(o.Volume)
	if volume == 0 {
		volume = 10
	}
	ease := 0.5
	if o.Difficulty >= 0 {
		ease = 1 - float64(o.Difficulty)/100
	}
	s := volume * (0.2 + ease)
	if o.Intent == "informational" {
		s *= 1.3
	}
	return s
}

// Use marks the opportunity taken by a story.
func (o *Opportunity) Use(storyID uuid.UUID, now time.Time) error {
	if o.Status != StatusOpen {
		return apperror.New(apperror.InvalidTransition, "the opportunity is not open")
	}
	o.Status, o.StoryID, o.UpdatedAt = StatusUsed, storyID, timeutil.Normalize(now)
	return nil
}

// Dismiss takes the opportunity out of the pool.
func (o *Opportunity) Dismiss(now time.Time) {
	o.Status, o.UpdatedAt = StatusDismissed, timeutil.Normalize(now)
}

// Reopen puts a dismissed or used opportunity back.
func (o *Opportunity) Reopen(now time.Time) {
	o.Status, o.StoryID, o.UpdatedAt = StatusOpen, uuid.Nil, timeutil.Normalize(now)
}

// Profile is the workspace's SEO context for refreshing opportunities.
type Profile struct {
	Domain   string `json:"domain"`   // e.g. example.com
	Country  string `json:"country"`  // e.g. TR
	Language string `json:"language"` // tr | en
	Topics   string `json:"topics"`   // what to look for

	// Direct OpenSEO research (with an API key).
	ProjectID     string   `json:"projectId,omitempty"`     // OpenSEO project the calls are scoped to
	Seeds         []string `json:"seeds,omitempty"`         // seed keywords, at most MaxSeeds
	Competitors   []string `json:"competitors,omitempty"`   // domains whose keywords to mine, at most MaxCompetitors
	MaxDifficulty int      `json:"maxDifficulty,omitempty"` // keyword difficulty ceiling; 0 means DefaultMaxDifficulty
	MinVolume     int      `json:"minVolume,omitempty"`     // monthly searches floor
}

// Limits of a direct OpenSEO refresh.
const (
	MaxSeeds             = 5
	MaxCompetitors       = 3
	DefaultMaxDifficulty = 50
	creditsPerSeed       = 54  // research_keywords at 150 results
	creditsPerCompetitor = 300 // get_domain_keyword_suggestions, upper end
)

// Difficulty returns the effective difficulty ceiling.
func (p Profile) Difficulty() int {
	if p.MaxDifficulty <= 0 || p.MaxDifficulty > 100 {
		return DefaultMaxDifficulty
	}
	return p.MaxDifficulty
}

// SeedList returns the seeds, or up to MaxSeeds topics (split on commas
// and lines) when no seeds are set.
func (p Profile) SeedList() []string {
	src := p.Seeds
	if len(src) == 0 {
		src = strings.FieldsFunc(p.Topics, func(r rune) bool { return r == ',' || r == '\n' || r == ';' })
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range src {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] || len(out) == MaxSeeds {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	return out
}

// EstimatedCredits is the most a direct refresh with p should spend.
func (p Profile) EstimatedCredits() int {
	return len(p.SeedList())*creditsPerSeed + len(p.Competitors)*creditsPerCompetitor
}

// Candidate is a researched keyword.
type Candidate struct {
	Keyword    string
	Volume     int // -1 unknown
	Difficulty int // -1 unknown
	Intent     string
	From       string // the seed or domain it came from
}

// Pick ranks candidates for educational content and returns the best n
// new ones: keywords already known (any status) or containing the brand
// are skipped, as are those above maxDifficulty or below minVolume.
// Informational intent ranks first; within it, more searches at lower
// difficulty win. Near-duplicates (same words in another order) are
// dropped.
func Pick(cands []Candidate, known []string, brand string, n, maxDifficulty, minVolume int) []Candidate {
	seen := map[string]bool{}
	for _, k := range known {
		seen[wordKey(k)] = true
	}
	brand = strings.ToLower(strings.TrimSpace(brand))
	type scored struct {
		c     Candidate
		score float64
	}
	var list []scored
	for _, c := range cands {
		c.Keyword = strings.Join(strings.Fields(c.Keyword), " ")
		key := wordKey(c.Keyword)
		switch {
		case key == "" || seen[key]:
			continue
		case brand != "" && strings.Contains(strings.ToLower(c.Keyword), brand):
			continue
		case c.Difficulty > maxDifficulty:
			continue
		case minVolume > 0 && c.Volume < minVolume:
			continue
		}
		seen[key] = true
		vol := math.Log10(float64(max(c.Volume, 0)) + 10)
		diff := float64(c.Difficulty)
		if diff < 0 {
			diff = float64(maxDifficulty) // unknown: assume the worst we accept
		}
		list = append(list, scored{c, vol * intentWeight(c.Intent) / (1 + diff/25)})
	}
	slices.SortStableFunc(list, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	out := make([]Candidate, 0, min(n, len(list)))
	for _, s := range list[:min(n, len(list))] {
		out = append(out, s.c)
	}
	return out
}

func intentWeight(intent string) float64 {
	switch strings.ToLower(intent) {
	case "informational":
		return 1.5
	case "commercial":
		return 1
	case "":
		return 0.9
	default: // navigational, transactional: rarely an article's job
		return 0.3
	}
}

// wordKey identifies a keyword regardless of case and word order.
func wordKey(k string) string {
	words := strings.Fields(strings.ToLower(k))
	slices.Sort(words)
	return strings.Join(words, " ")
}
