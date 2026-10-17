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
	PreviewQuality Quality `json:"previewQuality,omitempty"`
	// FinalQuality is the resolution of the final render after approval;
	// "" takes the host profile's final scale.
	FinalQuality Quality `json:"finalQuality,omitempty"`
	// PreviewFPS and FinalFPS are the frame rates of the review and final
	// renders; 0 takes the host profile's.
	PreviewFPS FrameRate `json:"previewFps,omitempty"`
	FinalFPS   FrameRate `json:"finalFps,omitempty"`
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
	if _, err := ParseFinalQuality(string(p.FinalQuality)); err != nil {
		return err
	}
	if _, err := ParsePreviewFPS(int(p.PreviewFPS)); err != nil {
		return err
	}
	if _, err := ParseFinalFPS(int(p.FinalFPS)); err != nil {
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

// Quality is the resolution a video is rendered at, named by the short
// side of the frame (a 1080p composition rendered at that scale).
type Quality string

const (
	Quality360p  Quality = "360p"
	Quality540p  Quality = "540p"
	Quality720p  Quality = "720p"
	Quality1080p Quality = "1080p"
	Quality1440p Quality = "1440p"
	Quality2160p Quality = "2160p"
)

// ParsePreviewQuality parses the quality of a review render, 360p to
// 1080p; "" is the default.
func ParsePreviewQuality(s string) (Quality, error) {
	switch q := Quality(s); q {
	case "", Quality360p, Quality540p, Quality720p, Quality1080p:
		return q, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "preview quality must be 360p, 540p, 720p or 1080p")
}

// ParseFinalQuality parses the quality of a final render, 720p to
// 2160p; "" is the default.
func ParseFinalQuality(s string) (Quality, error) {
	switch q := Quality(s); q {
	case "", Quality720p, Quality1080p, Quality1440p, Quality2160p:
		return q, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "final quality must be 720p, 1080p, 1440p or 2160p")
}

// Scale is the render scale of a 1080p composition; 0 for the default.
func (q Quality) Scale() float64 {
	switch q {
	case Quality360p:
		return 1.0 / 3
	case Quality540p:
		return 0.5
	case Quality720p:
		return 2.0 / 3
	case Quality1080p:
		return 1
	case Quality1440p:
		return 4.0 / 3
	case Quality2160p:
		return 2
	}
	return 0
}

// FrameRate is how many frames a second a video is rendered at. Every
// composition is a pure function of time, so it renders at any rate.
type FrameRate int

// DefaultFPS is the frame rate of a render nothing else sets.
const DefaultFPS FrameRate = 30

// ParsePreviewFPS parses the frame rate of a review render: 15, 24 or 30
// (fewer frames render faster and look choppier); 0 is the default.
func ParsePreviewFPS(n int) (FrameRate, error) {
	switch n {
	case 0, 15, 24, 30:
		return FrameRate(n), nil
	}
	return 0, apperror.New(apperror.ValidationFailed, "preview frame rate must be 15, 24 or 30")
}

// ParseFinalFPS parses the frame rate of a final render: 30 or 60; 0 is
// the default.
func ParseFinalFPS(n int) (FrameRate, error) {
	switch n {
	case 0, 30, 60:
		return FrameRate(n), nil
	}
	return 0, apperror.New(apperror.ValidationFailed, "final frame rate must be 30 or 60")
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
