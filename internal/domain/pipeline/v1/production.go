package pipeline

import (
	"regexp"
	"strings"

	"kurgu/internal/apperror"
)

// Production is how stories are made rather than what they are: settings
// a pipeline or an ad-hoc request sets for its stories, each overriding
// the workspace default. The zero value takes every default.
type Production struct {
	// Agent is the model and effort the authoring agent drafts with.
	Agent Agent `json:"agent,omitzero"`
}

// Check reports whether every setting is valid.
func (p Production) Check() error {
	_, err := NewAgent(p.Agent.Model, string(p.Agent.Effort))
	return err
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
