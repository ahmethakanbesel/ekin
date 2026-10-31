package analytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	analyticsdomain "github.com/ahmethakanbesel/ekin/internal/domain/analytics/v1"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// SQLite is the production Repository.
type SQLite struct{ q db.QuerierSource }

var _ Repository = (*SQLite)(nil)

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

func nullMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: timeutil.ToMillis(t), Valid: true}
}

func (r *SQLite) Connection(ctx context.Context, workspaceID uuid.UUID) (*analyticsdomain.Connection, error) {
	list, err := r.connections(ctx, `WHERE workspace_id = ?`, workspaceID.String())
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperror.New(apperror.NotFound, "search console is not connected")
	}
	return list[0], nil
}

func (r *SQLite) Connections(ctx context.Context) ([]*analyticsdomain.Connection, error) {
	return r.connections(ctx, `WHERE property != '' ORDER BY workspace_id`)
}

func (r *SQLite) PutConnection(ctx context.Context, c *analyticsdomain.Connection) error {
	sn := c.Snapshot()
	var by sql.NullString
	if sn.ConnectedBy != uuid.Nil {
		by = sql.NullString{String: sn.ConnectedBy.String(), Valid: true}
	}
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO search_connections
		(workspace_id, sealed_token, property, connected_by, connected_at, synced_through, last_sync_at, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (workspace_id) DO UPDATE SET sealed_token = excluded.sealed_token, property = excluded.property,
			connected_by = excluded.connected_by, connected_at = excluded.connected_at, synced_through = excluded.synced_through,
			last_sync_at = excluded.last_sync_at, last_error = excluded.last_error, updated_at = excluded.updated_at`,
		sn.WorkspaceID.String(), sn.SealedToken, sn.Property, by, timeutil.ToMillis(sn.ConnectedAt), nullMillis(sn.SyncedThrough),
		nullMillis(sn.LastSyncAt), sn.LastError, timeutil.ToMillis(sn.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put search connection: %w", err)
	}
	return nil
}

func (r *SQLite) DeleteConnection(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM search_connections WHERE workspace_id = ?`, workspaceID.String())
	return err
}

func (r *SQLite) connections(ctx context.Context, tail string, a ...any) (_ []*analyticsdomain.Connection, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT workspace_id, sealed_token, property, connected_by, connected_at, synced_through,
		last_sync_at, last_error, updated_at FROM search_connections `+tail, a...)
	if err != nil {
		return nil, fmt.Errorf("query search connections: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []*analyticsdomain.Connection
	for rows.Next() {
		var (
			sn               analyticsdomain.Snapshot
			ws               string
			by               sql.NullString
			connected, upd   int64
			through, lastRun sql.NullInt64
		)
		if err := rows.Scan(&ws, &sn.SealedToken, &sn.Property, &by, &connected, &through, &lastRun, &sn.LastError, &upd); err != nil {
			return nil, err
		}
		sn.WorkspaceID, _ = uuid.Parse(ws)
		if by.Valid {
			sn.ConnectedBy, _ = uuid.Parse(by.String)
		}
		sn.ConnectedAt, sn.UpdatedAt = timeutil.FromMillis(connected), timeutil.FromMillis(upd)
		sn.SyncedThrough, sn.LastSyncAt = timeutil.FromMillis(through.Int64), timeutil.FromMillis(lastRun.Int64)
		c, err := analyticsdomain.Rehydrate(sn)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *SQLite) ReplaceMetrics(ctx context.Context, workspaceID uuid.UUID, dim analyticsdomain.Dimension, start, end time.Time, ms []analyticsdomain.Metric) error {
	q := r.q.W(ctx)
	from, to := timeutil.ToMillis(analyticsdomain.Day(start)), timeutil.ToMillis(analyticsdomain.Day(end))
	if _, err := q.ExecContext(ctx, `DELETE FROM search_metrics WHERE workspace_id = ? AND dimension = ? AND day BETWEEN ? AND ?`,
		workspaceID.String(), string(dim), from, to); err != nil {
		return fmt.Errorf("clear search metrics: %w", err)
	}
	for _, m := range ms {
		d := timeutil.ToMillis(analyticsdomain.Day(m.Date))
		if d < from || d > to {
			continue
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO search_metrics (workspace_id, dimension, day, key, clicks, impressions, position)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO UPDATE SET clicks = excluded.clicks, impressions = excluded.impressions,
			position = excluded.position`, workspaceID.String(), string(dim), d, m.Key, m.Clicks, m.Impressions, m.Position); err != nil {
			return fmt.Errorf("put search metric: %w", err)
		}
	}
	return nil
}

func (r *SQLite) Metrics(ctx context.Context, workspaceID uuid.UUID, dim analyticsdomain.Dimension, keys []string, start, end time.Time) (_ []analyticsdomain.Metric, err error) {
	where := `workspace_id = ? AND dimension = ? AND day BETWEEN ? AND ?`
	a := []any{workspaceID.String(), string(dim), timeutil.ToMillis(analyticsdomain.Day(start)), timeutil.ToMillis(analyticsdomain.Day(end))}
	if len(keys) > 0 {
		where += ` AND key IN (` + strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + `)`
		for _, k := range keys {
			a = append(a, k)
		}
	}
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT day, key, clicks, impressions, position FROM search_metrics WHERE `+where+
		` ORDER BY day, key`, a...)
	if err != nil {
		return nil, fmt.Errorf("query search metrics: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []analyticsdomain.Metric
	for rows.Next() {
		var m analyticsdomain.Metric
		var d int64
		if err := rows.Scan(&d, &m.Key, &m.Clicks, &m.Impressions, &m.Position); err != nil {
			return nil, err
		}
		m.Date = timeutil.FromMillis(d)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *SQLite) Top(ctx context.Context, workspaceID uuid.UUID, dim analyticsdomain.Dimension, start, end time.Time, limit int) (_ []KeyTotals, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT key, SUM(clicks), SUM(impressions), SUM(position * impressions)
		FROM search_metrics WHERE workspace_id = ? AND dimension = ? AND day BETWEEN ? AND ?
		GROUP BY key ORDER BY SUM(clicks) DESC, SUM(impressions) DESC, key LIMIT ?`,
		workspaceID.String(), string(dim), timeutil.ToMillis(analyticsdomain.Day(start)), timeutil.ToMillis(analyticsdomain.Day(end)), limit)
	if err != nil {
		return nil, fmt.Errorf("top search keys: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	n := days(start, end)
	var out []KeyTotals
	for rows.Next() {
		var k KeyTotals
		var weighted float64
		if err := rows.Scan(&k.Key, &k.Clicks, &k.Impressions, &weighted); err != nil {
			return nil, err
		}
		if k.Impressions > 0 {
			k.Position = weighted / float64(k.Impressions)
		}
		k.Days = n
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *SQLite) ClearMetrics(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM search_metrics WHERE workspace_id = ?`, workspaceID.String())
	return err
}
