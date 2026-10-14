// Package seo models keyword opportunities: search terms worth writing
// for, collected from OpenSEO (or pasted in) and consumed one per story
// by pipelines whose source is "seo".
package seo

import (
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
}
