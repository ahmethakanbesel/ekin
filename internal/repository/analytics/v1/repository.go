// Package analytics persists Search Console connections and the daily
// search figures synced from them.
package analytics

import (
	"context"
	"time"

	"github.com/google/uuid"

	analyticsdomain "github.com/ahmethakanbesel/ekin/internal/domain/analytics/v1"
)

// KeyTotals is one page's or query's totals over a range.
type KeyTotals struct {
	Key string
	analyticsdomain.Totals
}

// Repository stores connections and metrics.
type Repository interface {
	// Connection returns NotFound when the workspace has none.
	Connection(ctx context.Context, workspaceID uuid.UUID) (*analyticsdomain.Connection, error)
	PutConnection(ctx context.Context, c *analyticsdomain.Connection) error
	DeleteConnection(ctx context.Context, workspaceID uuid.UUID) error
	// Connections returns every connection with a property.
	Connections(ctx context.Context) ([]*analyticsdomain.Connection, error)

	// ReplaceMetrics makes ms the workspace's figures of dimension for the
	// days start through end.
	ReplaceMetrics(ctx context.Context, workspaceID uuid.UUID, dim analyticsdomain.Dimension, start, end time.Time, ms []analyticsdomain.Metric) error
	// Metrics returns the figures of keys (all keys when empty) in
	// [start, end], by day.
	Metrics(ctx context.Context, workspaceID uuid.UUID, dim analyticsdomain.Dimension, keys []string, start, end time.Time) ([]analyticsdomain.Metric, error)
	// Top returns the keys with the most clicks (then impressions) in
	// [start, end], at most limit.
	Top(ctx context.Context, workspaceID uuid.UUID, dim analyticsdomain.Dimension, start, end time.Time, limit int) ([]KeyTotals, error)
	// ClearMetrics drops the workspace's figures.
	ClearMetrics(ctx context.Context, workspaceID uuid.UUID) error
}

func days(start, end time.Time) int {
	start, end = analyticsdomain.Day(start), analyticsdomain.Day(end)
	if end.Before(start) {
		return 0
	}
	return int(end.Sub(start)/(24*time.Hour)) + 1
}
