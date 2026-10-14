// Package notify persists the notification feed.
package notify

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/db"
	"kurgu/pkg/timeutil"
)

// Notification is one feed entry.
type Notification struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	StoryID     uuid.UUID
	Kind        string
	Title       string
	Detail      string
	CreatedAt   time.Time
	EmailedAt   time.Time
}

// Repository stores notifications.
type Repository interface {
	Add(ctx context.Context, n Notification) error
	// Feed returns the workspace's latest notifications, newest first.
	Feed(ctx context.Context, workspaceID uuid.UUID, limit int) ([]Notification, error)
	// Unsent returns notifications not yet emailed, oldest first.
	Unsent(ctx context.Context, limit int) ([]Notification, error)
	MarkEmailed(ctx context.Context, ids []uuid.UUID, at time.Time) error
}

// SQLite is the production Repository.
type SQLite struct{ q db.QuerierSource }

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

const columns = `id, workspace_id, story_id, kind, title, detail, created_at, emailed_at`

func (r *SQLite) Add(ctx context.Context, n Notification) error {
	var story sql.NullString
	if n.StoryID != uuid.Nil {
		story = sql.NullString{String: n.StoryID.String(), Valid: true}
	}
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO notifications (`+columns+`) VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		n.ID.String(), n.WorkspaceID.String(), story, n.Kind, n.Title, n.Detail, timeutil.ToMillis(n.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return nil
}

func (r *SQLite) Feed(ctx context.Context, workspaceID uuid.UUID, limit int) ([]Notification, error) {
	return r.query(ctx, `WHERE workspace_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, workspaceID.String(), limit)
}

func (r *SQLite) Unsent(ctx context.Context, limit int) ([]Notification, error) {
	return r.query(ctx, `WHERE emailed_at IS NULL ORDER BY created_at, id LIMIT ?`, limit)
}

func (r *SQLite) query(ctx context.Context, tail string, args ...any) ([]Notification, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+columns+` FROM notifications `+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Notification
	for rows.Next() {
		var (
			n            Notification
			rawID, rawWS string
			story        sql.NullString
			created      int64
			emailed      sql.NullInt64
		)
		if err := rows.Scan(&rawID, &rawWS, &story, &n.Kind, &n.Title, &n.Detail, &created, &emailed); err != nil {
			return nil, err
		}
		n.ID, _ = uuid.Parse(rawID)
		n.WorkspaceID, _ = uuid.Parse(rawWS)
		if story.Valid {
			n.StoryID, _ = uuid.Parse(story.String)
		}
		n.CreatedAt = timeutil.FromMillis(created)
		if emailed.Valid {
			n.EmailedAt = timeutil.FromMillis(emailed.Int64)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *SQLite) MarkEmailed(ctx context.Context, ids []uuid.UUID, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{timeutil.ToMillis(at)}
	for _, id := range ids {
		args = append(args, id.String())
	}
	_, err := r.q.W(ctx).ExecContext(ctx, `UPDATE notifications SET emailed_at = ? WHERE id IN (`+
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	return err
}

// InMemory is a Repository for tests.
type InMemory struct {
	mu   sync.Mutex
	rows []Notification
}

// NewInMemory returns an empty repository.
func NewInMemory() *InMemory { return &InMemory{} }

func (r *InMemory) Add(_ context.Context, n Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, n)
	return nil
}

func (r *InMemory) Feed(_ context.Context, workspaceID uuid.UUID, limit int) ([]Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Notification
	for i := len(r.rows) - 1; i >= 0 && len(out) < limit; i-- {
		if r.rows[i].WorkspaceID == workspaceID {
			out = append(out, r.rows[i])
		}
	}
	return out, nil
}

func (r *InMemory) Unsent(_ context.Context, limit int) ([]Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Notification
	for _, n := range r.rows {
		if n.EmailedAt.IsZero() && len(out) < limit {
			out = append(out, n)
		}
	}
	return out, nil
}

func (r *InMemory) MarkEmailed(_ context.Context, ids []uuid.UUID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if slices.Contains(ids, r.rows[i].ID) {
			r.rows[i].EmailedAt = at
		}
	}
	return nil
}
