// Package notify persists the notification feed.
package notify

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	"kurgu/pkg/page"
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

// FeedFilter narrows Feed and picks its page.
type FeedFilter struct {
	Kinds []string // empty means any
	Limit int      // 0 means DefaultFeedLimit; at most MaxFeedLimit
	After string   // the cursor a previous page ended with
}

// Feed limits.
const (
	DefaultFeedLimit = 50
	MaxFeedLimit     = 200
)

// cursor is the sort key of a page's last notification.
type cursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func nextCursor(last Notification) string {
	return page.Encode(cursor{At: timeutil.ToMillis(last.CreatedAt), ID: last.ID.String()})
}

// Repository stores notifications.
type Repository interface {
	Add(ctx context.Context, n Notification) error
	// Feed returns a page of the workspace's notifications, newest first,
	// and the cursor of the next page ("" after the last).
	Feed(ctx context.Context, workspaceID uuid.UUID, f FeedFilter) ([]Notification, string, error)
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

func (r *SQLite) Feed(ctx context.Context, workspaceID uuid.UUID, f FeedFilter) ([]Notification, string, error) {
	where := `WHERE workspace_id = ?`
	args := []any{workspaceID.String()}
	if len(f.Kinds) > 0 {
		where += ` AND kind IN (` + strings.TrimSuffix(strings.Repeat("?,", len(f.Kinds)), ",") + `)`
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	var after cursor
	if ok, err := page.Decode(f.After, &after); err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	} else if ok {
		where += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, after.At, after.At, after.ID)
	}
	limit := page.Limit(f.Limit, DefaultFeedLimit, MaxFeedLimit)
	out, err := r.query(ctx, where+` ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, limit+1)...)
	if err != nil || len(out) <= limit {
		return out, "", err
	}
	return out[:limit], nextCursor(out[limit-1]), nil
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

func (r *InMemory) Feed(_ context.Context, workspaceID uuid.UUID, f FeedFilter) ([]Notification, string, error) {
	var after cursor
	hasAfter, err := page.Decode(f.After, &after)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	}
	r.mu.Lock()
	var out []Notification
	for _, n := range r.rows {
		at := timeutil.ToMillis(n.CreatedAt)
		if n.WorkspaceID != workspaceID || len(f.Kinds) > 0 && !slices.Contains(f.Kinds, n.Kind) ||
			hasAfter && (at > after.At || at == after.At && n.ID.String() >= after.ID) {
			continue
		}
		out = append(out, n)
	}
	r.mu.Unlock()
	slices.SortFunc(out, func(a, b Notification) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(b.ID.String(), a.ID.String()))
	})
	limit := page.Limit(f.Limit, DefaultFeedLimit, MaxFeedLimit)
	if len(out) <= limit {
		return out, "", nil
	}
	return out[:limit], nextCursor(out[limit-1]), nil
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
