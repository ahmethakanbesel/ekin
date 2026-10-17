// Package library persists media library assets.
package library

import (
	"context"

	"github.com/google/uuid"

	librarydomain "kurgu/internal/domain/library/v1"
	"kurgu/pkg/page"
	"kurgu/pkg/timeutil"
)

// Filter narrows an asset listing and picks its page.
type Filter struct {
	Kinds        []librarydomain.Kind        // empty means any
	Origins      []librarydomain.Origin      // empty means any
	Statuses     []librarydomain.Status      // empty means any
	Orientations []librarydomain.Orientation // empty means any; music has none
	// Query keeps assets whose title, tags or description contain it,
	// compared folded (pkg/fold).
	Query string
	// Archived includes archived assets; they are left out otherwise.
	Archived bool
	Limit    int    // 0 means DefaultListLimit; at most MaxListLimit
	After    string // the cursor a previous page ended with
}

// Listing limits.
const (
	DefaultListLimit = 100
	MaxListLimit     = 500
)

// cursor is the sort key of a page's last asset.
type cursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func nextCursor(last *librarydomain.Asset) string {
	return page.Encode(cursor{At: timeutil.ToMillis(last.CreatedAt()), ID: last.ID().String()})
}

// Repository persists Asset aggregates.
type Repository interface {
	// Create returns Conflict when the workspace already holds a file
	// with the same SHA-256.
	Create(ctx context.Context, a *librarydomain.Asset) error
	Update(ctx context.Context, a *librarydomain.Asset) error
	// Get returns AssetNotFound when the asset is not in the workspace.
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*librarydomain.Asset, error)
	// GetByID ignores the workspace, for job handlers.
	GetByID(ctx context.Context, id uuid.UUID) (*librarydomain.Asset, error)
	// FindBySHA256 returns the workspace's asset with that file hash, or
	// AssetNotFound.
	FindBySHA256(ctx context.Context, workspaceID uuid.UUID, sha256 string) (*librarydomain.Asset, error)
	// List returns a page of the workspace's assets, newest first, and
	// the cursor of the next page ("" after the last).
	List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*librarydomain.Asset, string, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
}
