// Package library persists media library assets.
package library

import (
	"context"
	"time"

	"github.com/google/uuid"

	librarydomain "ekin/internal/domain/library/v1"
	"ekin/pkg/page"
	"ekin/pkg/timeutil"
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
	// FindBySourceRef returns the workspace's asset saved from that outside
	// item ("pexels:video:123"), or AssetNotFound.
	FindBySourceRef(ctx context.Context, workspaceID uuid.UUID, ref string) (*librarydomain.Asset, error)
	// Provisional lists the provisional assets saved from a deliverable's
	// drafts, oldest first.
	Provisional(ctx context.Context, workspaceID, deliverableID uuid.UUID) ([]*librarydomain.Asset, error)
	// GetCollect returns which kinds the workspace saves from its drafts;
	// nothing when never set.
	GetCollect(ctx context.Context, workspaceID uuid.UUID) (librarydomain.Collect, error)
	SaveCollect(ctx context.Context, workspaceID uuid.UUID, c librarydomain.Collect, now time.Time) error
	// List returns a page of the workspace's assets, newest first, and
	// the cursor of the next page ("" after the last).
	List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*librarydomain.Asset, string, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
	// RecordUsage stores a usage; recording the same one again is a no-op.
	RecordUsage(ctx context.Context, u librarydomain.Usage) error
	// Usages returns the asset's usages, newest first.
	Usages(ctx context.Context, workspaceID, assetID uuid.UUID) ([]librarydomain.Usage, error)
	// UsageCounts returns, per asset, how many deliverables used it;
	// assets never used are absent.
	UsageCounts(ctx context.Context, workspaceID uuid.UUID, assetIDs []uuid.UUID) (map[uuid.UUID]int, error)
}
