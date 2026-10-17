// Package seo persists keyword opportunities.
package seo

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	seodomain "kurgu/internal/domain/seo/v1"
	"kurgu/pkg/timeutil"
)

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
	// List returns the workspace's opportunities with status (all when
	// empty), newest first.
	List(ctx context.Context, workspaceID uuid.UUID, status seodomain.Status) ([]seodomain.Opportunity, error)
}

// SQLite is the production Repository.
type SQLite struct{ q db.QuerierSource }

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

const cols = `id, workspace_id, keyword, norm, language, volume, difficulty, intent, note, source, status, story_id, created_at, updated_at`

func (r *SQLite) Add(ctx context.Context, o seodomain.Opportunity) (bool, error) {
	res, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO opportunities (`+cols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, args(o)...)
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

func (r *SQLite) List(ctx context.Context, workspaceID uuid.UUID, status seodomain.Status) ([]seodomain.Opportunity, error) {
	if status == "" {
		return r.query(ctx, `WHERE workspace_id = ? ORDER BY created_at DESC, id DESC LIMIT 1000`, workspaceID.String())
	}
	return r.query(ctx, `WHERE workspace_id = ? AND status = ? ORDER BY created_at DESC, id DESC LIMIT 1000`, workspaceID.String(), string(status))
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

func (r *InMemory) List(_ context.Context, ws uuid.UUID, status seodomain.Status) ([]seodomain.Opportunity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []seodomain.Opportunity
	for i := len(r.rows) - 1; i >= 0; i-- {
		o := r.rows[i]
		if o.WorkspaceID == ws && (status == "" || o.Status == status) {
			out = append(out, o)
		}
	}
	slices.SortStableFunc(out, func(a, b seodomain.Opportunity) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}
