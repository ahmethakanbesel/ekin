// Package story persists stories, their deliverables and revisions.
package story

import (
	"context"

	"github.com/google/uuid"

	storydomain "kurgu/internal/domain/story/v1"
)

// Filter narrows a story listing.
type Filter struct {
	Statuses   []storydomain.Status // story summary status
	PipelineID uuid.UUID
	Limit      int
}

// Match is a stored story similar to a candidate idea.
type Match struct {
	StoryID uuid.UUID
	Title   string
	Angle   string
	// Score is the trigram similarity in [0, 1] of fingerprints.
	Score float64
}

// Repository persists Story aggregates.
type Repository interface {
	Create(ctx context.Context, s *storydomain.Story) error
	// Save writes s if its stored version is still s.Version(), then
	// bumps it; otherwise StaleVersion.
	Save(ctx context.Context, s *storydomain.Story) error
	// Get returns StoryNotFound when the story is not in the workspace.
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*storydomain.Story, error)
	GetByID(ctx context.Context, id uuid.UUID) (*storydomain.Story, error)
	// List returns stories newest first.
	List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*storydomain.Story, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error

	// PutRevision inserts or replaces a revision.
	PutRevision(ctx context.Context, rev storydomain.Revision) error
	Revisions(ctx context.Context, deliverableID uuid.UUID) ([]storydomain.Revision, error)
	// Revision returns NotFound when it does not exist.
	Revision(ctx context.Context, deliverableID uuid.UUID, number int) (storydomain.Revision, error)

	// Similar returns the workspace's stories whose title or angle share
	// trigrams with text, best first.
	Similar(ctx context.Context, workspaceID uuid.UUID, text string, limit int) ([]Match, error)
}
