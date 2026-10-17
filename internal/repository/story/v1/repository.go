// Package story persists stories, their deliverables and revisions.
package story

import (
	"context"

	"github.com/google/uuid"

	storydomain "kurgu/internal/domain/story/v1"
)

// Filter narrows a story listing and picks its page.
type Filter struct {
	Statuses   []storydomain.Status // story summary status
	PipelineID uuid.UUID
	Origin     storydomain.Origin // "" means any
	// Query keeps stories whose title contains it, compared folded
	// (storydomain.Fingerprint): case, Turkish i and accents aside.
	Query string
	Limit int    // 0 means DefaultListLimit; at most MaxListLimit
	After string // the cursor a previous page ended with
}

// Listing limits.
const (
	DefaultListLimit = 100
	MaxListLimit     = 500
)

// cursor is the sort key of a listing's last story.
type cursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
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
	// List returns a page of stories, newest first, and the cursor of the
	// next page ("" after the last).
	List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*storydomain.Story, string, error)
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
