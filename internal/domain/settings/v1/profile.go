// Package settings models runtime-editable instance settings. A host
// profile says how hard the workers using it may work (PLAN.md §4); each
// worker picks its profile by name (KURGU_WORKER_PROFILE), so the VPS and
// a Mac can be throttled differently.
package settings

import (
	"regexp"
	"slices"
	"time"

	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
)

// Profile is a host profile.
type Profile struct {
	Name           string                  `json:"name"`
	Concurrency    map[jobdomain.Queue]int `json:"concurrency"`
	HeavyExclusive bool                    `json:"heavyExclusive"`
	// HeavyQueues are the queues HeavyExclusive runs one job at a time
	// across; nil means jobdomain.DefaultHeavy. A Mac can drop agent:
	// a draft mostly waits on Claude and barely uses the CPU.
	HeavyQueues []jobdomain.Queue `json:"heavyQueues"`
	Cooldown    time.Duration     `json:"cooldown"`
	Paused      bool              `json:"paused"`
	// CPULimit caps the job processes (agent, browser, encoder, speech)
	// at this percent of the host's CPUs; 0 means no cap. Linux only.
	CPULimit int `json:"cpuLimit"`
	// Nice is the scheduling priority (0 normal to 19 lowest) of the job
	// processes, so the host's own work comes first; the Kurgu server
	// keeps its priority.
	Nice int `json:"nice"`
	// RenderDuty is the share of wall time spent rendering (0.25 sleeps
	// three frame-times after every frame).
	RenderDuty   float64 `json:"renderDuty"`
	RenderChunks int     `json:"renderChunks"`
	PreviewScale float64 `json:"previewScale"`
	FinalScale   float64 `json:"finalScale"`
	// PreviewFPS and FinalFPS are the frame rates of review and final
	// renders; 0 (a profile saved before they existed) is 30.
	PreviewFPS int `json:"previewFps,omitempty"`
	FinalFPS   int `json:"finalFps,omitempty"`
}

// PreviewRate is the frame rate of review renders.
func (p Profile) PreviewRate() int { return rate(p.PreviewFPS) }

// FinalRate is the frame rate of final renders.
func (p Profile) FinalRate() int { return rate(p.FinalFPS) }

func rate(fps int) int {
	if fps == 0 {
		return int(pipelinedomain.DefaultFPS)
	}
	return fps
}

// MaxNice is the lowest scheduling priority.
const MaxNice = 19

// Heavy returns the heavy queues in effect.
func (p Profile) Heavy() []jobdomain.Queue {
	if p.HeavyQueues == nil {
		return slices.Clone(jobdomain.DefaultHeavy)
	}
	return slices.Clone(p.HeavyQueues)
}

// MinCPULimit is the lowest CPU limit, in percent: below it jobs barely
// move and their timeouts start to bite.
const MinCPULimit = 10

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
	for _, q := range p.HeavyQueues {
		if _, err := jobdomain.ParseQueue(string(q)); err != nil {
			return fail("unknown heavy queue " + string(q))
		}
	}
	switch {
	case p.Nice < 0 || p.Nice > MaxNice:
		return fail("priority (nice) must be 0 to 19")
	case p.Cooldown < 0 || p.Cooldown > time.Hour:
		return fail("cooldown must be between 0 and 1 hour")
	case p.CPULimit != 0 && (p.CPULimit < MinCPULimit || p.CPULimit > 100):
		return fail("CPU limit must be 0 (none) or 10 to 100 percent")
	case p.RenderDuty < 0.05 || p.RenderDuty > 1:
		return fail("render duty must be between 0.05 and 1")
	case p.RenderChunks < 1 || p.RenderChunks > 16:
		return fail("render chunks must be 1 to 16")
	case p.PreviewScale < 0.25 || p.PreviewScale > 1:
		return fail("preview scale must be between 0.25 and 1")
	case p.FinalScale < 0.25 || p.FinalScale > 2:
		return fail("final scale must be between 0.25 and 2")
	}
	if _, err := pipelinedomain.ParsePreviewFPS(p.PreviewFPS); err != nil {
		return err
	}
	_, err := pipelinedomain.ParseFinalFPS(p.FinalFPS)
	return err
}

// Key is the settings key the profile is stored under.
func (p Profile) Key() string { return "profile:" + p.Name }
