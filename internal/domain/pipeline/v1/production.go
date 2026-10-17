package pipeline

import (
	"regexp"
	"strings"

	"kurgu/internal/apperror"
	librarydomain "kurgu/internal/domain/library/v1"
)

// Production is how stories are made rather than what they are: settings
// a pipeline or an ad-hoc request sets for its stories, each overriding
// the workspace default. The zero value takes every default.
type Production struct {
	// Agent is the model and effort the authoring agent drafts with.
	Agent Agent `json:"agent,omitzero"`
	// PreviewQuality is the resolution of the review render; "" takes
	// the host profile's preview scale.
	PreviewQuality PreviewQuality `json:"previewQuality,omitempty"`
	// ReviewAt is when a person first reviews a video: the finished video
	// (the default) or, first, its script.
	ReviewAt ReviewPoint `json:"reviewAt,omitempty"`
	// Media says, per kind, whether music and stock are made fresh, come
	// from the workspace's library, or both.
	Media librarydomain.Policy `json:"media,omitzero"`
}

// Check reports whether every setting is valid.
func (p Production) Check() error {
	if _, err := NewAgent(p.Agent.Model, string(p.Agent.Effort)); err != nil {
		return err
	}
	if _, err := ParsePreviewQuality(string(p.PreviewQuality)); err != nil {
		return err
	}
	if _, err := ParseReviewPoint(string(p.ReviewAt)); err != nil {
		return err
	}
	return p.Media.Check()
}

// CheckFor reports whether the settings fit outputs: reviewing scripts
// needs a video.
func (p Production) CheckFor(outputs Outputs) error {
	if err := p.Check(); err != nil {
		return err
	}
	if _, video := outputs.Video(); p.ReviewAt == ReviewScript && !video {
		return apperror.New(apperror.ValidationFailed, "reviewing the script first needs a video output")
	}
	return nil
}

// ReviewPoint is when a person first reviews a video.
type ReviewPoint string

const (
	// ReviewVideo reviews the rendered preview (the default).
	ReviewVideo ReviewPoint = ""
	// ReviewScript reviews the script first; once approved, the video is
	// built from it and reviewed as usual.
	ReviewScript ReviewPoint = "script"
)

// ParseReviewPoint parses a review point; "" and "video" are the default.
func ParseReviewPoint(s string) (ReviewPoint, error) {
	switch s {
	case "", "video":
		return ReviewVideo, nil
	case string(ReviewScript):
		return ReviewScript, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "review point must be video or script")
}

// PreviewQuality is the resolution a video is rendered at for review,
// named by the short side of a 1080p frame.
type PreviewQuality string

const (
	Preview360p  PreviewQuality = "360p"
	Preview540p  PreviewQuality = "540p"
	Preview720p  PreviewQuality = "720p"
	Preview1080p PreviewQuality = "1080p"
)

// ParsePreviewQuality parses a preview quality; "" is the default.
func ParsePreviewQuality(s string) (PreviewQuality, error) {
	switch q := PreviewQuality(s); q {
	case "", Preview360p, Preview540p, Preview720p, Preview1080p:
		return q, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "preview quality must be 360p, 540p, 720p or 1080p")
}

// Scale is the render scale of a 1080p composition; 0 for the default.
func (q PreviewQuality) Scale() float64 {
	switch q {
	case Preview360p:
		return 1.0 / 3
	case Preview540p:
		return 0.5
	case Preview720p:
		return 2.0 / 3
	case Preview1080p:
		return 1
	}
	return 0
}

// Effort is how hard the authoring agent thinks (Claude Code's --effort).
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// ParseEffort parses an effort; "" is the default.
func ParseEffort(s string) (Effort, error) {
	switch e := Effort(s); e {
	case "", EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return e, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "effort must be low, medium, high, xhigh or max")
}

// Agent picks the authoring agent's model and effort for drafting; an
// empty field takes the workspace default (System → Credentials, then
// KURGU_CLAUDE_MODEL / KURGU_CLAUDE_EFFORT).
type Agent struct {
	Model  string `json:"model,omitempty"`
	Effort Effort `json:"effort,omitempty"`
}

// modelPattern accepts what Claude Code takes for --model: an alias
// ("sonnet") or a model id ("claude-opus-5-5"), with an optional
// context suffix ("opus[1m]"). It keeps anything else off the command
// line.
var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}(\[[0-9a-z]{1,8}\])?$`)

// NewAgent checks a drafting model and effort; blanks are defaults.
func NewAgent(model, effort string) (Agent, error) {
	model = strings.TrimSpace(model)
	if model != "" && !modelPattern.MatchString(model) {
		return Agent{}, apperror.New(apperror.ValidationFailed, "model must be a Claude model id or alias, such as claude-sonnet-5-5")
	}
	e, err := ParseEffort(effort)
	if err != nil {
		return Agent{}, err
	}
	return Agent{Model: model, Effort: e}, nil
}
