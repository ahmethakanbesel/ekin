package pipeline

import (
	"github.com/google/uuid"

	"kurgu/internal/apperror"
)

// TemplateChoice is which house style a pipeline's or an ad-hoc
// request's stories use: a chosen template, explicitly none, or (the
// zero value) the workspace's default template.
type TemplateChoice struct {
	// ID is the chosen template; Nil when none is chosen.
	ID uuid.UUID
	// None asks for no template, whatever the workspace's default.
	None bool
}

// NewTemplateChoice checks a choice: a template and "no template" at
// once contradict each other.
func NewTemplateChoice(id uuid.UUID, none bool) (TemplateChoice, error) {
	if none && id != uuid.Nil {
		return TemplateChoice{}, apperror.New(apperror.ValidationFailed, "pick a template or no template, not both")
	}
	return TemplateChoice{ID: id, None: none}, nil
}

// Over resolves the choice over the workspace's default template
// (Nil when it has none): the template to use, Nil for none.
func (c TemplateChoice) Over(workspaceDefault uuid.UUID) uuid.UUID {
	switch {
	case c.ID != uuid.Nil:
		return c.ID
	case c.None:
		return uuid.Nil
	}
	return workspaceDefault
}
