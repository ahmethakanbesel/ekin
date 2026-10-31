package site

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/page"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

const competitorCols = `id, workspace_id, name, url, pages, last_check_at, last_error, next_check_at, created_at`

func (r *SQLite) Competitors(ctx context.Context, workspaceID uuid.UUID) ([]*sitedomain.Competitor, error) {
	return r.competitors(ctx, `WHERE workspace_id = ? ORDER BY name COLLATE NOCASE, id`, workspaceID.String())
}

func (r *SQLite) Competitor(ctx context.Context, workspaceID, id uuid.UUID) (*sitedomain.Competitor, error) {
	list, err := r.competitors(ctx, `WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperror.New(apperror.NotFound, "competitor not found")
	}
	return list[0], nil
}

func (r *SQLite) DueCompetitors(ctx context.Context, now time.Time) ([]*sitedomain.Competitor, error) {
	return r.competitors(ctx, `WHERE next_check_at <= ? ORDER BY next_check_at`, timeutil.ToMillis(now))
}

func (r *SQLite) PutCompetitor(ctx context.Context, c *sitedomain.Competitor) error {
	sn := c.Snapshot()
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO site_competitors (`+competitorCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, pages = excluded.pages, last_check_at = excluded.last_check_at,
			last_error = excluded.last_error, next_check_at = excluded.next_check_at`,
		sn.ID.String(), sn.WorkspaceID.String(), sn.Name, sn.URL, sn.Pages, nullMillis(sn.LastCheckAt), sn.LastError,
		timeutil.ToMillis(sn.NextCheckAt), timeutil.ToMillis(sn.CreatedAt))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return apperror.New(apperror.Conflict, "that competitor is followed already")
		}
		return fmt.Errorf("put competitor: %w", err)
	}
	return nil
}

func (r *SQLite) DeleteCompetitor(ctx context.Context, workspaceID, id uuid.UUID) error {
	res, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM site_competitors WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.NotFound, "competitor not found")
	}
	return nil
}

func (r *SQLite) competitors(ctx context.Context, tail string, a ...any) (_ []*sitedomain.Competitor, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+competitorCols+` FROM site_competitors `+tail, a...)
	if err != nil {
		return nil, fmt.Errorf("query competitors: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []*sitedomain.Competitor
	for rows.Next() {
		var (
			sn            sitedomain.CompetitorSnapshot
			id, ws        string
			last          sql.NullInt64
			next, created int64
		)
		if err := rows.Scan(&id, &ws, &sn.Name, &sn.URL, &sn.Pages, &last, &sn.LastError, &next, &created); err != nil {
			return nil, err
		}
		sn.ID, _ = uuid.Parse(id)
		sn.WorkspaceID, _ = uuid.Parse(ws)
		sn.LastCheckAt, sn.NextCheckAt, sn.CreatedAt = timeutil.FromMillis(last.Int64), timeutil.FromMillis(next), timeutil.FromMillis(created)
		c, err := sitedomain.RehydrateCompetitor(sn)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *SQLite) CompetitorPages(ctx context.Context, competitorID uuid.UUID) (_ []sitedomain.CompetitorPage, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT url, title, first_seen, last_seen, removed_at FROM site_competitor_pages
		WHERE competitor_id = ? ORDER BY url`, competitorID.String())
	if err != nil {
		return nil, fmt.Errorf("query competitor pages: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []sitedomain.CompetitorPage
	for rows.Next() {
		var (
			p           sitedomain.CompetitorPage
			first, last int64
			removed     sql.NullInt64
		)
		if err := rows.Scan(&p.URL, &p.Title, &first, &last, &removed); err != nil {
			return nil, err
		}
		p.FirstSeen, p.LastSeen, p.RemovedAt = timeutil.FromMillis(first), timeutil.FromMillis(last), timeutil.FromMillis(removed.Int64)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SQLite) ReplaceCompetitorPages(ctx context.Context, competitorID uuid.UUID, pages []sitedomain.CompetitorPage) error {
	q := r.q.W(ctx)
	if _, err := q.ExecContext(ctx, `DELETE FROM site_competitor_pages WHERE competitor_id = ?`, competitorID.String()); err != nil {
		return fmt.Errorf("clear competitor pages: %w", err)
	}
	for _, p := range pages {
		if _, err := q.ExecContext(ctx, `INSERT INTO site_competitor_pages (competitor_id, url, title, first_seen, last_seen, removed_at)
			VALUES (?, ?, ?, ?, ?, ?)`, competitorID.String(), p.URL, p.Title, timeutil.ToMillis(p.FirstSeen), timeutil.ToMillis(p.LastSeen),
			nullMillis(p.RemovedAt)); err != nil {
			return fmt.Errorf("put competitor page: %w", err)
		}
	}
	return nil
}

func (r *SQLite) AddCompetitorChanges(ctx context.Context, changes []sitedomain.CompetitorChange) error {
	q := r.q.W(ctx)
	for _, c := range changes {
		if _, err := q.ExecContext(ctx, `INSERT INTO site_competitor_changes (id, workspace_id, competitor_id, kind, url, title, at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, c.ID.String(), c.WorkspaceID.String(), c.CompetitorID.String(), string(c.Kind), c.URL, c.Title,
			timeutil.ToMillis(c.At)); err != nil {
			return fmt.Errorf("add competitor change: %w", err)
		}
	}
	return nil
}

func (r *SQLite) CompetitorChanges(ctx context.Context, workspaceID uuid.UUID, f ChangeFilter) (_ []sitedomain.CompetitorChange, _ string, err error) {
	where := []string{"workspace_id = ?"}
	a := []any{workspaceID.String()}
	if f.CompetitorID != uuid.Nil {
		where = append(where, "competitor_id = ?")
		a = append(a, f.CompetitorID.String())
	}
	if f.Kind != "" {
		where = append(where, "kind = ?")
		a = append(a, string(f.Kind))
	}
	var after changeCursor
	if ok, err := page.Decode(f.After, &after); err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	} else if ok {
		where = append(where, "(at < ? OR (at = ? AND id < ?))")
		a = append(a, after.At, after.At, after.ID)
	}
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT id, workspace_id, competitor_id, kind, url, title, at FROM site_competitor_changes
		WHERE `+strings.Join(where, " AND ")+` ORDER BY at DESC, id DESC LIMIT ?`, append(a, limit+1)...)
	if err != nil {
		return nil, "", fmt.Errorf("query competitor changes: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []sitedomain.CompetitorChange
	for rows.Next() {
		var (
			c           sitedomain.CompetitorChange
			id, ws, cid string
			kind        string
			at          int64
		)
		if err := rows.Scan(&id, &ws, &cid, &kind, &c.URL, &c.Title, &at); err != nil {
			return nil, "", err
		}
		c.ID, _ = uuid.Parse(id)
		c.WorkspaceID, _ = uuid.Parse(ws)
		c.CompetitorID, _ = uuid.Parse(cid)
		c.Kind, c.At = sitedomain.ChangeKind(kind), timeutil.FromMillis(at)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil || len(out) <= limit {
		return out, "", err
	}
	last := out[limit-1]
	return out[:limit], page.Encode(changeCursor{At: timeutil.ToMillis(last.At), ID: last.ID.String()}), nil
}
