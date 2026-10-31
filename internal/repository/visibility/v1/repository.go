// Package visibility persists AI visibility settings, prompts, runs and
// answers.
package visibility

import (
	"context"
	"time"

	"github.com/google/uuid"

	visibilitydomain "github.com/ahmethakanbesel/ekin/internal/domain/visibility/v1"
)

// Repository stores a workspace's visibility checks.
type Repository interface {
	// Profile returns NotFound when the workspace has none.
	Profile(ctx context.Context, workspaceID uuid.UUID) (*visibilitydomain.Profile, error)
	PutProfile(ctx context.Context, p *visibilitydomain.Profile) error
	// DueProfiles returns the profiles whose scheduled run is due at now.
	DueProfiles(ctx context.Context, now time.Time) ([]*visibilitydomain.Profile, error)

	// Prompts returns the workspace's prompts, oldest first.
	Prompts(ctx context.Context, workspaceID uuid.UUID) ([]visibilitydomain.Prompt, error)
	// Prompt returns NotFound when it is not the workspace's.
	Prompt(ctx context.Context, workspaceID, id uuid.UUID) (visibilitydomain.Prompt, error)
	PutPrompt(ctx context.Context, p visibilitydomain.Prompt) error
	DeletePrompt(ctx context.Context, workspaceID, id uuid.UUID) error

	CreateRun(ctx context.Context, r visibilitydomain.Run) error
	SaveRun(ctx context.Context, r visibilitydomain.Run) error
	// Run returns NotFound when it is not the workspace's.
	Run(ctx context.Context, workspaceID, id uuid.UUID) (visibilitydomain.Run, error)
	// Runs returns the latest runs, newest first.
	Runs(ctx context.Context, workspaceID uuid.UUID, limit int) ([]visibilitydomain.Run, error)

	PutAnswer(ctx context.Context, workspaceID uuid.UUID, a visibilitydomain.Answer) error
	// Answers returns a run's answers, by prompt then model.
	Answers(ctx context.Context, workspaceID, runID uuid.UUID) ([]visibilitydomain.Answer, error)
	// CallsSince counts the calls (answers, failed ones too) made since.
	CallsSince(ctx context.Context, workspaceID uuid.UUID, since time.Time) (int, error)
	// ForgetTexts empties the text of answers made before cutoff and
	// returns how many.
	ForgetTexts(ctx context.Context, cutoff time.Time) (int, error)
}
