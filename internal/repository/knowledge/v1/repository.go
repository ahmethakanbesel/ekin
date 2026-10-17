// Package knowledge persists documents and their extracted pages.
package knowledge

import (
	"context"

	"github.com/google/uuid"

	knowledgedomain "kurgu/internal/domain/knowledge/v1"
	"kurgu/pkg/page"
	"kurgu/pkg/timeutil"
)

// Filter narrows a document listing and picks its page.
type Filter struct {
	Statuses []knowledgedomain.Status // empty means any
	Formats  []knowledgedomain.Format // empty means any
	// Query keeps documents whose title contains it, compared folded
	// (pkg/fold): case, Turkish i and accents aside.
	Query string
	Limit int    // 0 means DefaultListLimit; at most MaxListLimit
	After string // the cursor a previous page ended with
}

// Listing limits.
const (
	DefaultListLimit = 500
	MaxListLimit     = 500
)

// cursor is the sort key of a page's last document.
type cursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func nextCursor(last *knowledgedomain.Document) string {
	return page.Encode(cursor{At: timeutil.ToMillis(last.CreatedAt()), ID: last.ID().String()})
}

// Repository persists Document aggregates and their pages.
type Repository interface {
	Create(ctx context.Context, d *knowledgedomain.Document) error
	Update(ctx context.Context, d *knowledgedomain.Document) error
	// Get returns DocumentNotFound when the document is not in the
	// workspace.
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*knowledgedomain.Document, error)
	// GetByID ignores the workspace, for job handlers.
	GetByID(ctx context.Context, id uuid.UUID) (*knowledgedomain.Document, error)
	// List returns a page of the workspace's documents, newest first, and
	// the cursor of the next page ("" after the last).
	List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*knowledgedomain.Document, string, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
	// ReplacePages swaps the document's pages for pages.
	ReplacePages(ctx context.Context, documentID uuid.UUID, pages []knowledgedomain.Page) error
	// Pages returns pages from..to inclusive (1-based); to 0 means to the end.
	Pages(ctx context.Context, documentID uuid.UUID, from, to int) ([]knowledgedomain.Page, error)
	// ReplaceHeadings swaps the document's table of contents for hs.
	ReplaceHeadings(ctx context.Context, documentID uuid.UUID, hs []knowledgedomain.Heading) error
	// Headings returns the table of contents in document order.
	Headings(ctx context.Context, documentID uuid.UUID) ([]knowledgedomain.Heading, error)
}
