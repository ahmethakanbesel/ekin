// Package site persists sites, their crawls, the latest crawl's pages
// and the audit's findings.
package site

import (
	"context"
	"time"

	"github.com/google/uuid"

	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/page"
)

// Listing limits.
const (
	DefaultListLimit = 100
	MaxListLimit     = 500
)

// PageFilter narrows a listing of pages and picks its page.
type PageFilter struct {
	Class sitedomain.StatusClass // "" means any
	// Query keeps pages whose address or title contains it, compared
	// folded (pkg/fold).
	Query string
	// URLs keeps only the pages at these addresses; empty means any.
	URLs  []string
	Limit int
	After string
}

// PageRow is a page and how many open findings it has.
type PageRow struct {
	sitedomain.Page
	OpenFindings int
}

// FindingFilter narrows a listing of findings and picks its page.
type FindingFilter struct {
	Statuses []sitedomain.FindingStatus // empty means any
	Rule     sitedomain.Rule            // "" means any
	Severity sitedomain.Severity        // "" means any
	URL      string                     // "" means any
	Limit    int
	After    string
}

// RuleCount is how many findings of a rule have a status.
type RuleCount struct {
	Rule  sitedomain.Rule
	Count int
}

// Repository stores a workspace's site.
type Repository interface {
	// Site returns NotFound when the workspace has none.
	Site(ctx context.Context, workspaceID uuid.UUID) (*sitedomain.Site, error)
	PutSite(ctx context.Context, s *sitedomain.Site) error
	// DueSites returns the sites whose scheduled crawl is due at now.
	DueSites(ctx context.Context, now time.Time) ([]*sitedomain.Site, error)

	CreateCrawl(ctx context.Context, c *sitedomain.Crawl) error
	SaveCrawl(ctx context.Context, c *sitedomain.Crawl) error
	// Crawl returns NotFound when it is not the workspace's.
	Crawl(ctx context.Context, workspaceID, id uuid.UUID) (*sitedomain.Crawl, error)
	// Crawls returns the workspace's latest crawls, newest first.
	Crawls(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*sitedomain.Crawl, error)

	// ReplacePages makes pages the workspace's inventory.
	ReplacePages(ctx context.Context, workspaceID uuid.UUID, pages []sitedomain.Page) error
	// FirstSeen returns when each page now in the inventory was first seen.
	FirstSeen(ctx context.Context, workspaceID uuid.UUID) (map[string]time.Time, error)
	// Pages returns a page of the inventory by address, and the cursor
	// of the next ("" after the last).
	Pages(ctx context.Context, workspaceID uuid.UUID, f PageFilter) ([]PageRow, string, error)

	// AllFindings returns every finding of the workspace.
	AllFindings(ctx context.Context, workspaceID uuid.UUID) ([]*sitedomain.Finding, error)
	// PutFindings inserts or updates findings.
	PutFindings(ctx context.Context, fs []*sitedomain.Finding) error
	// Finding returns NotFound when it is not the workspace's.
	Finding(ctx context.Context, workspaceID, id uuid.UUID) (*sitedomain.Finding, error)
	// Findings returns a page of findings, most severe first, then by
	// rule and address, and the cursor of the next ("" after the last).
	Findings(ctx context.Context, workspaceID uuid.UUID, f FindingFilter) ([]*sitedomain.Finding, string, error)
	// RuleCounts counts the workspace's findings with status per rule.
	RuleCounts(ctx context.Context, workspaceID uuid.UUID, status sitedomain.FindingStatus) ([]RuleCount, error)
	// ClearInventory deletes the pages and findings, for a site that
	// moved to another address.
	ClearInventory(ctx context.Context, workspaceID uuid.UUID) error
}

type pageCursor struct {
	URL string `json:"u"`
}

type findingCursor struct {
	Rank int    `json:"r"`
	Rule string `json:"k"`
	URL  string `json:"u"`
}

func encodeFinding(f *sitedomain.Finding) string {
	return page.Encode(findingCursor{Rank: f.Severity().Rank(), Rule: string(f.Rule()), URL: f.URL()})
}

// pageData is the JSON stored for a page beside its indexed columns.
type pageData struct {
	Error       string   `json:"error,omitempty"`
	RedirectTo  string   `json:"redirectTo,omitempty"`
	ContentType string   `json:"contentType,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Canonical   string   `json:"canonical,omitempty"`
	Lang        string   `json:"lang,omitempty"`
	H1          []string `json:"h1,omitempty"`
	Noindex     bool     `json:"noindex,omitempty"`
	Words       int      `json:"words,omitempty"`
	Types       []string `json:"types,omitempty"`
	Links       []string `json:"links,omitempty"`
	InSitemap   bool     `json:"inSitemap,omitempty"`
	Depth       int      `json:"depth"`
}

func toData(p sitedomain.Page) pageData {
	return pageData{Error: p.Error, RedirectTo: p.RedirectTo, ContentType: p.ContentType, Title: p.Title,
		Description: p.Description, Canonical: p.Canonical, Lang: p.Lang, H1: p.H1, Noindex: p.Noindex, Words: p.Words,
		Types: p.Types, Links: p.Links, InSitemap: p.InSitemap, Depth: p.Depth}
}

func (d pageData) page(url string, status int, first, last time.Time) sitedomain.Page {
	return sitedomain.Page{URL: url, Status: status, Error: d.Error, RedirectTo: d.RedirectTo, ContentType: d.ContentType,
		Title: d.Title, Description: d.Description, Canonical: d.Canonical, Lang: d.Lang, H1: d.H1, Noindex: d.Noindex,
		Words: d.Words, Types: d.Types, Links: d.Links, InSitemap: d.InSitemap, Depth: d.Depth, FirstSeen: first, LastSeen: last}
}
