// Package settings models runtime-editable instance settings. A host
// profile says how hard the workers using it may work (PLAN.md §4); each
// worker picks its profile by name (KURGU_WORKER_PROFILE), so the VPS and
// a Mac can be throttled differently.
package settings

import (
	"regexp"
	"time"

	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
)

// Profile is a host profile.
type Profile struct {
	Name           string                  `json:"name"`
	Concurrency    map[jobdomain.Queue]int `json:"concurrency"`
	HeavyExclusive bool                    `json:"heavyExclusive"`
	Cooldown       time.Duration           `json:"cooldown"`
	Paused         bool                    `json:"paused"`
	// RenderDuty is the share of wall time spent rendering (0.25 sleeps
	// three frame-times after every frame).
	RenderDuty   float64 `json:"renderDuty"`
	RenderChunks int     `json:"renderChunks"`
	PreviewScale float64 `json:"previewScale"`
	FinalScale   float64 `json:"finalScale"`
}

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Validate checks every field.
func (p Profile) Validate() error {
	fail := func(msg string) error { return apperror.New(apperror.ValidationFailed, msg) }
	if !profileName.MatchString(p.Name) {
		return fail("profile name must be lowercase letters, digits and hyphens")
	}
	for q, n := range p.Concurrency {
		if _, err := jobdomain.ParseQueue(string(q)); err != nil {
			return fail("unknown queue " + string(q))
		}
		if n < 0 || n > 8 {
			return fail("queue concurrency must be 0 to 8")
		}
	}
	switch {
	case p.Cooldown < 0 || p.Cooldown > time.Hour:
		return fail("cooldown must be between 0 and 1 hour")
	case p.RenderDuty < 0.05 || p.RenderDuty > 1:
		return fail("render duty must be between 0.05 and 1")
	case p.RenderChunks < 1 || p.RenderChunks > 16:
		return fail("render chunks must be 1 to 16")
	case p.PreviewScale < 0.25 || p.PreviewScale > 1:
		return fail("preview scale must be between 0.25 and 1")
	case p.FinalScale < 0.25 || p.FinalScale > 2:
		return fail("final scale must be between 0.25 and 2")
	}
	return nil
}

// Key is the settings key the profile is stored under.
func (p Profile) Key() string { return "profile:" + p.Name }
