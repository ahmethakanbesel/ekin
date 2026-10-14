// Package template persists workspace templates (built-ins live in code).
package template

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	templatedomain "kurgu/internal/domain/template/v1"
	"kurgu/pkg/timeutil"
)

// Repository stores workspace templates.
type Repository interface {
	Save(ctx context.Context, t templatedomain.Template) error
	// Get returns NotFound when the template is not in the workspace.
	Get(ctx context.Context, workspaceID, id uuid.UUID) (templatedomain.Template, error)
	List(ctx context.Context, workspaceID uuid.UUID) ([]templatedomain.Template, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
}

// SQLite is the production Repository.
type SQLite struct{ q db.QuerierSource }

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

func notFound() error { return apperror.New(apperror.NotFound, "template not found") }

func (r *SQLite) Save(ctx context.Context, t templatedomain.Template) error {
	spec, _ := json.Marshal(t.Spec())
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO templates (id, workspace_id, spec, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (id) DO UPDATE SET spec = excluded.spec, updated_at = excluded.updated_at`,
		t.ID.String(), t.WorkspaceID.String(), string(spec), string(t.Source), timeutil.ToMillis(t.CreatedAt), timeutil.ToMillis(t.UpdatedAt))
	if err != nil {
		return fmt.Errorf("save template: %w", err)
	}
	return nil
}

func (r *SQLite) Get(ctx context.Context, workspaceID, id uuid.UUID) (templatedomain.Template, error) {
	list, err := r.query(ctx, `WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return templatedomain.Template{}, err
	}
	if len(list) == 0 {
		return templatedomain.Template{}, notFound()
	}
	return list[0], nil
}

func (r *SQLite) List(ctx context.Context, workspaceID uuid.UUID) ([]templatedomain.Template, error) {
	return r.query(ctx, `WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID.String())
}

func (r *SQLite) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	res, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM templates WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound()
	}
	return nil
}

func (r *SQLite) query(ctx context.Context, tail string, args ...any) ([]templatedomain.Template, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT id, workspace_id, spec, source, created_at, updated_at FROM templates `+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("query templates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []templatedomain.Template
	for rows.Next() {
		var (
			rawID, rawWS, spec, source string
			created, updated           int64
		)
		if err := rows.Scan(&rawID, &rawWS, &spec, &source, &created, &updated); err != nil {
			return nil, err
		}
		var s templatedomain.Spec
		if err := json.Unmarshal([]byte(spec), &s); err != nil {
			return nil, fmt.Errorf("decode template: %w", err)
		}
		id, _ := uuid.Parse(rawID)
		ws, _ := uuid.Parse(rawWS)
		t, err := templatedomain.New(id, ws, s, templatedomain.Source(source), timeutil.FromMillis(created))
		if err != nil {
			return nil, err
		}
		t.UpdatedAt = timeutil.FromMillis(updated)
		out = append(out, t)
	}
	return out, rows.Err()
}

// InMemory is a Repository for tests.
type InMemory struct {
	mu   sync.Mutex
	rows []templatedomain.Template
}

// NewInMemory returns an empty repository.
func NewInMemory() *InMemory { return &InMemory{} }

func (r *InMemory) Save(_ context.Context, t templatedomain.Template) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if r.rows[i].ID == t.ID {
			r.rows[i] = t
			return nil
		}
	}
	r.rows = append(r.rows, t)
	return nil
}

func (r *InMemory) Get(_ context.Context, ws, id uuid.UUID) (templatedomain.Template, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.rows {
		if t.WorkspaceID == ws && t.ID == id {
			return t, nil
		}
	}
	return templatedomain.Template{}, notFound()
}

func (r *InMemory) List(_ context.Context, ws uuid.UUID) ([]templatedomain.Template, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []templatedomain.Template
	for _, t := range r.rows {
		if t.WorkspaceID == ws {
			out = append(out, t)
		}
	}
	return slices.Clone(out), nil
}

func (r *InMemory) Delete(_ context.Context, ws, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, t := range r.rows {
		if t.WorkspaceID == ws && t.ID == id {
			r.rows = append(r.rows[:i], r.rows[i+1:]...)
			return nil
		}
	}
	return notFound()
}
