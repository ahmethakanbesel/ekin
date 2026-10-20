// Package seo persists keyword opportunities.
package seo

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	seodomain "github.com/ahmethakanbesel/ekin/internal/domain/seo/v1"
	"github.com/ahmethakanbesel/ekin/pkg/fold"
	"github.com/ahmethakanbesel/ekin/pkg/page"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// Filter narrows a listing of the pool and picks its page.
type Filter struct {
	Statuses []seodomain.Status // empty means any
	Language string             // "" means any
	// Query keeps keywords containing it, compared folded (pkg/fold).
	Query string
	Limit int    // 0 means DefaultListLimit; at most MaxListLimit
	After string // the cursor a previous page ended with
}

// Listing limits.
const (
	DefaultListLimit = 1000
	MaxListLimit     = 1000
)

// cursor is the sort key of a page's last opportunity: status rank,
// score, id.
type cursor struct {
	Rank  int     `json:"r"`
	Score float64 `json:"s"`
	ID    string  `json:"i"`
}

func nextCursor(last seodomain.Opportunity) string {
	return page.Encode(cursor{Rank: last.Status.Rank(), Score: last.Score(), ID: last.ID.String()})
}

// Repository stores opportunities.
type Repository interface {
	// Add inserts o unless the workspace already has the keyword; it
	// reports whether it inserted.
	Add(ctx context.Context, o seodomain.Opportunity) (bool, error)
	// Save stores o's status if the stored status is still from, so two
	// callers never both act on one opportunity (two runs using one
	// keyword, a dismissal undoing a use); otherwise Conflict.
	Save(ctx context.Context, o seodomain.Opportunity, from seodomain.Status) error
	Get(ctx context.Context, workspaceID, id uuid.UUID) (seodomain.Opportunity, error)
	// List returns a page of the workspace's pool, open first then used
	// then dismissed, best first (seodomain.Opportunity.Score) within
	// each, and the cursor of the next page ("" after the last).
	List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]seodomain.Opportunity, string, error)
	// Keywords returns every keyword in the workspace's pool.
	Keywords(ctx context.Context, workspaceID uuid.UUID) ([]string, error)
}

// SQLite is the production Repository.
type SQLite struct{ q db.QuerierSource }

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

const cols = `id, workspace_id, keyword, norm, language, volume, difficulty, intent, note, source, status, story_id, created_at, updated_at`

func (r *SQLite) Add(ctx context.Context, o seodomain.Opportunity) (bool, error) {
	res, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO opportunities (`+cols+`, score) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, append(args(o), o.Score())...)
	if err != nil {
		return false, fmt.Errorf("insert opportunity: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (r *SQLite) Save(ctx context.Context, o seodomain.Opportunity, from seodomain.Status) error {
	var story sql.NullString
	if o.StoryID != uuid.Nil {
		story = sql.NullString{String: o.StoryID.String(), Valid: true}
	}
	res, err := r.q.W(ctx).ExecContext(ctx, `UPDATE opportunities SET status = ?, story_id = ?, updated_at = ?
		WHERE workspace_id = ? AND id = ? AND status = ?`,
		string(o.Status), story, timeutil.ToMillis(o.UpdatedAt), o.WorkspaceID.String(), o.ID.String(), string(from))
	if err != nil {
		return fmt.Errorf("update opportunity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := r.Get(ctx, o.WorkspaceID, o.ID); err != nil {
			return err
		}
		return changed()
	}
	return nil
}

func changed() error {
	return apperror.New(apperror.Conflict, "the opportunity changed meanwhile; reload and try again")
}

func (r *SQLite) Get(ctx context.Context, workspaceID, id uuid.UUID) (seodomain.Opportunity, error) {
	list, err := r.query(ctx, `WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return seodomain.Opportunity{}, err
	}
	if len(list) == 0 {
		return seodomain.Opportunity{}, apperror.New(apperror.NotFound, "opportunity not found")
	}
	return list[0], nil
}

// rankSQL mirrors seodomain.Status.Rank.
const rankSQL = `CASE status WHEN 'open' THEN 0 WHEN 'used' THEN 1 ELSE 2 END`

func (r *SQLite) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]seodomain.Opportunity, string, error) {
	where := []string{"workspace_id = ?"}
	a := []any{workspaceID.String()}
	if len(f.Statuses) > 0 {
		where = append(where, "status IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",")+")")
		for _, st := range f.Statuses {
			a = append(a, string(st))
		}
	}
	if f.Language != "" {
		where = append(where, "language = ?")
		a = append(a, f.Language)
	}
	if q := fold.String(f.Query); q != "" {
		where = append(where, "instr(norm, ?) > 0")
		a = append(a, q)
	}
	var after cursor
	if ok, err := page.Decode(f.After, &after); err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	} else if ok {
		where = append(where, `(`+rankSQL+` > ? OR (`+rankSQL+` = ? AND (score < ? OR (score = ? AND id < ?))))`)
		a = append(a, after.Rank, after.Rank, after.Score, after.Score, after.ID)
	}
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	out, err := r.query(ctx, `WHERE `+strings.Join(where, " AND ")+` ORDER BY `+rankSQL+`, score DESC, id DESC LIMIT ?`,
		append(a, limit+1)...)
	if err != nil || len(out) <= limit {
		return out, "", err
	}
	last := out[limit-1]
	// The cursor carries the stored score, which the order compares.
	c := cursor{Rank: last.Status.Rank(), ID: last.ID.String()}
	if err := r.q.R(ctx).QueryRowContext(ctx, `SELECT score FROM opportunities WHERE id = ?`, c.ID).Scan(&c.Score); err != nil {
		return nil, "", fmt.Errorf("score of the last opportunity: %w", err)
	}
	return out[:limit], page.Encode(c), nil
}

func (r *SQLite) Keywords(ctx context.Context, workspaceID uuid.UUID) ([]string, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT keyword FROM opportunities WHERE workspace_id = ? ORDER BY keyword`, workspaceID.String())
	if err != nil {
		return nil, fmt.Errorf("query keywords: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *SQLite) query(ctx context.Context, tail string, a ...any) ([]seodomain.Opportunity, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+cols+` FROM opportunities `+tail, a...)
	if err != nil {
		return nil, fmt.Errorf("query opportunities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []seodomain.Opportunity
	for rows.Next() {
		var (
			o                seodomain.Opportunity
			rawID, rawWS     string
			norm, status     string
			story            sql.NullString
			created, updated int64
		)
		if err := rows.Scan(&rawID, &rawWS, &o.Keyword, &norm, &o.Language, &o.Volume, &o.Difficulty, &o.Intent, &o.Note, &o.Source,
			&status, &story, &created, &updated); err != nil {
			return nil, err
		}
		o.ID, _ = uuid.Parse(rawID)
		o.WorkspaceID, _ = uuid.Parse(rawWS)
		o.Status = seodomain.Status(status)
		if story.Valid {
			o.StoryID, _ = uuid.Parse(story.String)
		}
		o.CreatedAt, o.UpdatedAt = timeutil.FromMillis(created), timeutil.FromMillis(updated)
		out = append(out, o)
	}
	return out, rows.Err()
}

func args(o seodomain.Opportunity) []any {
	var story sql.NullString
	if o.StoryID != uuid.Nil {
		story = sql.NullString{String: o.StoryID.String(), Valid: true}
	}
	return []any{o.ID.String(), o.WorkspaceID.String(), o.Keyword, o.Norm(), o.Language, o.Volume, o.Difficulty, o.Intent, o.Note,
		o.Source, string(o.Status), story, timeutil.ToMillis(o.CreatedAt), timeutil.ToMillis(o.UpdatedAt)}
}

// InMemory is a Repository for tests.
type InMemory struct {
	mu   sync.Mutex
	rows []seodomain.Opportunity
}

// NewInMemory returns an empty repository.
func NewInMemory() *InMemory { return &InMemory{} }

func (r *InMemory) Add(_ context.Context, o seodomain.Opportunity) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.rows {
		if x.WorkspaceID == o.WorkspaceID && x.Key() == o.Key() {
			return false, nil
		}
	}
	r.rows = append(r.rows, o)
	return true, nil
}

func (r *InMemory) Save(_ context.Context, o seodomain.Opportunity, from seodomain.Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if r.rows[i].WorkspaceID == o.WorkspaceID && r.rows[i].ID == o.ID {
			if r.rows[i].Status != from {
				return changed()
			}
			r.rows[i] = o
			return nil
		}
	}
	return apperror.New(apperror.NotFound, "opportunity not found")
}

func (r *InMemory) Get(_ context.Context, ws, id uuid.UUID) (seodomain.Opportunity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.rows {
		if o.WorkspaceID == ws && o.ID == id {
			return o, nil
		}
	}
	return seodomain.Opportunity{}, apperror.New(apperror.NotFound, "opportunity not found")
}

func (r *InMemory) List(_ context.Context, ws uuid.UUID, f Filter) ([]seodomain.Opportunity, string, error) {
	var after cursor
	hasAfter, err := page.Decode(f.After, &after)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	}
	query := fold.String(f.Query)
	r.mu.Lock()
	var out []seodomain.Opportunity
	for _, o := range r.rows {
		if o.WorkspaceID != ws || len(f.Statuses) > 0 && !slices.Contains(f.Statuses, o.Status) ||
			f.Language != "" && o.Language != f.Language || query != "" && !strings.Contains(o.Norm(), query) {
			continue
		}
		if hasAfter && compare(o, after) <= 0 {
			continue
		}
		out = append(out, o)
	}
	r.mu.Unlock()
	slices.SortFunc(out, func(a, b seodomain.Opportunity) int {
		return compare(a, cursor{Rank: b.Status.Rank(), Score: b.Score(), ID: b.ID.String()})
	})
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	if len(out) <= limit {
		return out, "", nil
	}
	return out[:limit], nextCursor(out[limit-1]), nil
}

// compare is negative when o comes before c in the pool's order, positive
// when after.
func compare(o seodomain.Opportunity, c cursor) int {
	return -cmp.Or(cmp.Compare(c.Rank, o.Status.Rank()), cmp.Compare(o.Score(), c.Score), cmp.Compare(o.ID.String(), c.ID))
}

func (r *InMemory) Keywords(_ context.Context, ws uuid.UUID) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, o := range r.rows {
		if o.WorkspaceID == ws {
			out = append(out, o.Keyword)
		}
	}
	slices.Sort(out)
	return out, nil
}
