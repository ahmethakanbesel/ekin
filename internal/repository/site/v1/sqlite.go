package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/fold"
	"github.com/ahmethakanbesel/ekin/pkg/page"
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

func nullID(id uuid.UUID) sql.NullString {
	if id == uuid.Nil {
		return sql.NullString{}
	}
	return sql.NullString{String: id.String(), Valid: true}
}

func parseNullID(s sql.NullString) uuid.UUID {
	if !s.Valid {
		return uuid.Nil
	}
	id, _ := uuid.Parse(s.String)
	return id
}

// ---- Site ----

func (r *SQLite) Site(ctx context.Context, workspaceID uuid.UUID) (*sitedomain.Site, error) {
	list, err := r.sites(ctx, `WHERE workspace_id = ?`, workspaceID.String())
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperror.New(apperror.NotFound, "no site is set up")
	}
	return list[0], nil
}

func (r *SQLite) PutSite(ctx context.Context, s *sitedomain.Site) error {
	sn := s.Snapshot()
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO sites (workspace_id, url, max_pages, schedule, next_crawl_at, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (workspace_id) DO UPDATE SET url = excluded.url, max_pages = excluded.max_pages, schedule = excluded.schedule,
			next_crawl_at = excluded.next_crawl_at, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		sn.WorkspaceID.String(), sn.URL, sn.MaxPages, sn.Schedule, nullMillis(sn.NextCrawlAt), nullID(sn.UpdatedBy),
		timeutil.ToMillis(sn.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put site: %w", err)
	}
	return nil
}

func (r *SQLite) DueSites(ctx context.Context, now time.Time) ([]*sitedomain.Site, error) {
	return r.sites(ctx, `WHERE next_crawl_at IS NOT NULL AND next_crawl_at <= ? ORDER BY next_crawl_at`, timeutil.ToMillis(now))
}

func (r *SQLite) sites(ctx context.Context, tail string, a ...any) (_ []*sitedomain.Site, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT workspace_id, url, max_pages, schedule, next_crawl_at, updated_by, updated_at FROM sites `+tail, a...)
	if err != nil {
		return nil, fmt.Errorf("query sites: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []*sitedomain.Site
	for rows.Next() {
		var (
			sn        sitedomain.Snapshot
			ws        string
			next      sql.NullInt64
			by        sql.NullString
			updatedAt int64
		)
		if err := rows.Scan(&ws, &sn.URL, &sn.MaxPages, &sn.Schedule, &next, &by, &updatedAt); err != nil {
			return nil, err
		}
		sn.WorkspaceID, _ = uuid.Parse(ws)
		sn.NextCrawlAt, sn.UpdatedBy, sn.UpdatedAt = timeutil.FromMillis(next.Int64), parseNullID(by), timeutil.FromMillis(updatedAt)
		s, err := sitedomain.Rehydrate(sn)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- Crawls ----

const crawlCols = `id, workspace_id, url, status, trigger, requested_by, pages, facts, failure, created_at, started_at, finished_at`

func crawlArgs(c *sitedomain.Crawl) ([]any, error) {
	sn := c.Snapshot()
	facts, err := json.Marshal(sn.Facts)
	if err != nil {
		return nil, err
	}
	return []any{sn.ID.String(), sn.WorkspaceID.String(), sn.URL, sn.Status, sn.Trigger, nullID(sn.RequestedBy), sn.Pages,
		string(facts), sn.Failure, timeutil.ToMillis(sn.CreatedAt), nullMillis(sn.StartedAt), nullMillis(sn.FinishedAt)}, nil
}

func (r *SQLite) CreateCrawl(ctx context.Context, c *sitedomain.Crawl) error {
	a, err := crawlArgs(c)
	if err != nil {
		return err
	}
	if _, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO site_crawls (`+crawlCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, a...); err != nil {
		return fmt.Errorf("insert crawl: %w", err)
	}
	return nil
}

func (r *SQLite) SaveCrawl(ctx context.Context, c *sitedomain.Crawl) error {
	a, err := crawlArgs(c)
	if err != nil {
		return err
	}
	res, err := r.q.W(ctx).ExecContext(ctx, `UPDATE site_crawls SET status = ?, pages = ?, facts = ?, failure = ?, started_at = ?, finished_at = ?
		WHERE id = ? AND workspace_id = ?`, a[3], a[6], a[7], a[8], a[10], a[11], a[0], a[1])
	if err != nil {
		return fmt.Errorf("update crawl: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.NotFound, "crawl not found")
	}
	return nil
}

func (r *SQLite) Crawl(ctx context.Context, workspaceID, id uuid.UUID) (*sitedomain.Crawl, error) {
	list, err := r.crawls(ctx, `WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperror.New(apperror.NotFound, "crawl not found")
	}
	return list[0], nil
}

func (r *SQLite) Crawls(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*sitedomain.Crawl, error) {
	return r.crawls(ctx, `WHERE workspace_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, workspaceID.String(), page.Limit(limit, 20, MaxListLimit))
}

func (r *SQLite) crawls(ctx context.Context, tail string, a ...any) (_ []*sitedomain.Crawl, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+crawlCols+` FROM site_crawls `+tail, a...)
	if err != nil {
		return nil, fmt.Errorf("query crawls: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []*sitedomain.Crawl
	for rows.Next() {
		var (
			sn               sitedomain.CrawlSnapshot
			id, ws, facts    string
			by               sql.NullString
			created          int64
			started, finishd sql.NullInt64
		)
		if err := rows.Scan(&id, &ws, &sn.URL, &sn.Status, &sn.Trigger, &by, &sn.Pages, &facts, &sn.Failure, &created, &started, &finishd); err != nil {
			return nil, err
		}
		sn.ID, _ = uuid.Parse(id)
		sn.WorkspaceID, _ = uuid.Parse(ws)
		sn.RequestedBy = parseNullID(by)
		if err := json.Unmarshal([]byte(facts), &sn.Facts); err != nil {
			return nil, fmt.Errorf("crawl facts: %w", err)
		}
		sn.CreatedAt, sn.StartedAt, sn.FinishedAt = timeutil.FromMillis(created), timeutil.FromMillis(started.Int64), timeutil.FromMillis(finishd.Int64)
		c, err := sitedomain.RehydrateCrawl(sn)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- Pages ----

func (r *SQLite) ReplacePages(ctx context.Context, workspaceID uuid.UUID, pages []sitedomain.Page) error {
	q := r.q.W(ctx)
	if _, err := q.ExecContext(ctx, `DELETE FROM site_pages WHERE workspace_id = ?`, workspaceID.String()); err != nil {
		return fmt.Errorf("clear pages: %w", err)
	}
	for _, p := range pages {
		data, err := json.Marshal(toData(p))
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO site_pages (workspace_id, url, status, class, search, data, first_seen, last_seen)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, workspaceID.String(), p.URL, p.Status, string(p.Class()), fold.String(p.URL+" "+p.Title),
			string(data), timeutil.ToMillis(p.FirstSeen), timeutil.ToMillis(p.LastSeen)); err != nil {
			return fmt.Errorf("insert page: %w", err)
		}
	}
	return nil
}

func (r *SQLite) FirstSeen(ctx context.Context, workspaceID uuid.UUID) (_ map[string]time.Time, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT url, first_seen FROM site_pages WHERE workspace_id = ?`, workspaceID.String())
	if err != nil {
		return nil, fmt.Errorf("query first seen: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	out := map[string]time.Time{}
	for rows.Next() {
		var u string
		var ms int64
		if err := rows.Scan(&u, &ms); err != nil {
			return nil, err
		}
		out[u] = timeutil.FromMillis(ms)
	}
	return out, rows.Err()
}

func (r *SQLite) Pages(ctx context.Context, workspaceID uuid.UUID, f PageFilter) (_ []PageRow, _ string, err error) {
	where := []string{"p.workspace_id = ?"}
	a := []any{workspaceID.String()}
	if f.Class != "" {
		where = append(where, "p.class = ?")
		a = append(a, string(f.Class))
	}
	if q := fold.String(f.Query); q != "" {
		where = append(where, "instr(p.search, ?) > 0")
		a = append(a, q)
	}
	var after pageCursor
	if ok, err := page.Decode(f.After, &after); err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	} else if ok {
		where = append(where, "p.url > ?")
		a = append(a, after.URL)
	}
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT p.url, p.status, p.data, p.first_seen, p.last_seen,
		(SELECT COUNT(*) FROM site_findings f WHERE f.workspace_id = p.workspace_id AND f.url = p.url AND f.status = 'open')
		FROM site_pages p WHERE `+strings.Join(where, " AND ")+` ORDER BY p.url LIMIT ?`, append(a, limit+1)...)
	if err != nil {
		return nil, "", fmt.Errorf("query pages: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []PageRow
	for rows.Next() {
		var (
			u, raw      string
			status, n   int
			first, last int64
			d           pageData
		)
		if err := rows.Scan(&u, &status, &raw, &first, &last, &n); err != nil {
			return nil, "", err
		}
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return nil, "", fmt.Errorf("page data: %w", err)
		}
		out = append(out, PageRow{Page: d.page(u, status, timeutil.FromMillis(first), timeutil.FromMillis(last)), OpenFindings: n})
	}
	if err := rows.Err(); err != nil || len(out) <= limit {
		return out, "", err
	}
	return out[:limit], page.Encode(pageCursor{URL: out[limit-1].URL}), nil
}

// ---- Findings ----

const findingCols = `id, workspace_id, rule, url, status, evidence, crawl_id, first_seen, last_seen, resolved_at, updated_at`

func (r *SQLite) PutFindings(ctx context.Context, fs []*sitedomain.Finding) error {
	q := r.q.W(ctx)
	for _, f := range fs {
		sn := f.Snapshot()
		ev, err := json.Marshal(sn.Evidence)
		if err != nil {
			return err
		}
		if sn.Evidence == nil {
			ev = []byte("[]")
		}
		_, err = q.ExecContext(ctx, `INSERT INTO site_findings (`+findingCols+`, severity_rank) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET status = excluded.status, evidence = excluded.evidence, crawl_id = excluded.crawl_id,
				last_seen = excluded.last_seen, resolved_at = excluded.resolved_at, updated_at = excluded.updated_at,
				severity_rank = excluded.severity_rank`,
			sn.ID.String(), sn.WorkspaceID.String(), sn.Rule, sn.URL, sn.Status, string(ev), nullID(sn.CrawlID),
			timeutil.ToMillis(sn.FirstSeen), timeutil.ToMillis(sn.LastSeen), nullMillis(sn.ResolvedAt), timeutil.ToMillis(sn.UpdatedAt),
			f.Severity().Rank())
		if err != nil {
			return fmt.Errorf("put finding: %w", err)
		}
	}
	return nil
}

func (r *SQLite) AllFindings(ctx context.Context, workspaceID uuid.UUID) ([]*sitedomain.Finding, error) {
	return r.findings(ctx, `WHERE workspace_id = ? ORDER BY severity_rank, rule, url`, workspaceID.String())
}

func (r *SQLite) Finding(ctx context.Context, workspaceID, id uuid.UUID) (*sitedomain.Finding, error) {
	list, err := r.findings(ctx, `WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperror.New(apperror.NotFound, "finding not found")
	}
	return list[0], nil
}

func (r *SQLite) Findings(ctx context.Context, workspaceID uuid.UUID, f FindingFilter) ([]*sitedomain.Finding, string, error) {
	where := []string{"workspace_id = ?"}
	a := []any{workspaceID.String()}
	if len(f.Statuses) > 0 {
		where = append(where, "status IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",")+")")
		for _, st := range f.Statuses {
			a = append(a, string(st))
		}
	}
	if f.Rule != "" {
		where = append(where, "rule = ?")
		a = append(a, string(f.Rule))
	}
	if f.Severity != "" {
		where = append(where, "severity_rank = ?")
		a = append(a, f.Severity.Rank())
	}
	if f.URL != "" {
		where = append(where, "url = ?")
		a = append(a, f.URL)
	}
	var after findingCursor
	if ok, err := page.Decode(f.After, &after); err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	} else if ok {
		where = append(where, "(severity_rank > ? OR (severity_rank = ? AND (rule > ? OR (rule = ? AND url > ?))))")
		a = append(a, after.Rank, after.Rank, after.Rule, after.Rule, after.URL)
	}
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	out, err := r.findings(ctx, `WHERE `+strings.Join(where, " AND ")+` ORDER BY severity_rank, rule, url LIMIT ?`, append(a, limit+1)...)
	if err != nil || len(out) <= limit {
		return out, "", err
	}
	return out[:limit], encodeFinding(out[limit-1]), nil
}

func (r *SQLite) RuleCounts(ctx context.Context, workspaceID uuid.UUID, status sitedomain.FindingStatus) (_ []RuleCount, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT rule, COUNT(*) FROM site_findings WHERE workspace_id = ? AND status = ?
		GROUP BY rule ORDER BY MIN(severity_rank), rule`, workspaceID.String(), string(status))
	if err != nil {
		return nil, fmt.Errorf("count findings: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []RuleCount
	for rows.Next() {
		var c RuleCount
		if err := rows.Scan(&c.Rule, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *SQLite) ClearInventory(ctx context.Context, workspaceID uuid.UUID) error {
	q := r.q.W(ctx)
	for _, table := range []string{"site_pages", "site_findings"} {
		if _, err := q.ExecContext(ctx, `DELETE FROM `+table+` WHERE workspace_id = ?`, workspaceID.String()); err != nil { //nolint:gosec // fixed table names
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	return nil
}

func (r *SQLite) findings(ctx context.Context, tail string, a ...any) (_ []*sitedomain.Finding, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+findingCols+` FROM site_findings `+tail, a...)
	if err != nil {
		return nil, fmt.Errorf("query findings: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []*sitedomain.Finding
	for rows.Next() {
		var (
			sn                   sitedomain.FindingSnapshot
			id, ws, ev           string
			crawl                sql.NullString
			first, last, updated int64
			resolved             sql.NullInt64
		)
		if err := rows.Scan(&id, &ws, &sn.Rule, &sn.URL, &sn.Status, &ev, &crawl, &first, &last, &resolved, &updated); err != nil {
			return nil, err
		}
		sn.ID, _ = uuid.Parse(id)
		sn.WorkspaceID, _ = uuid.Parse(ws)
		sn.CrawlID = parseNullID(crawl)
		if err := json.Unmarshal([]byte(ev), &sn.Evidence); err != nil {
			return nil, fmt.Errorf("finding evidence: %w", err)
		}
		sn.FirstSeen, sn.LastSeen, sn.ResolvedAt, sn.UpdatedAt = timeutil.FromMillis(first), timeutil.FromMillis(last),
			timeutil.FromMillis(resolved.Int64), timeutil.FromMillis(updated)
		f, err := sitedomain.RehydrateFinding(sn)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
