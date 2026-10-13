// Package pipeline persists pipelines.
package pipeline

import (
	"context"
	"time"

	"github.com/google/uuid"

	pipelinedomain "kurgu/internal/domain/pipeline/v1"
)

// Repository persists Pipeline aggregates. Every method except ListDue is
// scoped to one workspace (CONVENTIONS.md §5).
type Repository interface {
	Create(ctx context.Context, p *pipelinedomain.Pipeline) error
	// Update stores p if the stored version still equals p.Version() and
	// returns the stored aggregate at the next version. A concurrent
	// change surfaces as StaleVersion.
	Update(ctx context.Context, p *pipelinedomain.Pipeline) (*pipelinedomain.Pipeline, error)
	// Get returns PipelineNotFound when id is not in workspaceID.
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*pipelinedomain.Pipeline, error)
	// List returns a workspace's pipelines, oldest first.
	List(ctx context.Context, workspaceID uuid.UUID) ([]*pipelinedomain.Pipeline, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
	// ListDue returns up to limit pipelines, from every workspace, whose
	// next run is at or before now, earliest first.
	ListDue(ctx context.Context, now time.Time, limit int) ([]*pipelinedomain.Pipeline, error)
}
