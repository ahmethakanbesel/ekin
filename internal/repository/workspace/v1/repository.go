// Package workspace persists workspaces.
package workspace

import (
	"context"

	"github.com/google/uuid"

	workspacedomain "kurgu/internal/domain/workspace/v1"
)

// Repository persists Workspace aggregates.
type Repository interface {
	Create(ctx context.Context, w *workspacedomain.Workspace) error
	Update(ctx context.Context, w *workspacedomain.Workspace) error
	// Get returns WorkspaceNotFound when id does not exist.
	Get(ctx context.Context, id uuid.UUID) (*workspacedomain.Workspace, error)
	// List returns every workspace, oldest first.
	List(ctx context.Context) ([]*workspacedomain.Workspace, error)
}
