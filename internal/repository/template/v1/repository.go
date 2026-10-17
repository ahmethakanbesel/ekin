// Package template persists workspace templates (built-ins live in code).
package template

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/internal/db"
	templatedomain "ekin/internal/domain/template/v1"
	"ekin/pkg/timeutil"
)

// Repository stores workspace templates.
type Repository interface {
	Create(ctx context.Context, t templatedomain.Template) error
	// Update stores t if the stored version is still t.Version and
	// returns it at the next version: StaleVersion when someone saved
	// meanwhile, NotFound when it was deleted (it is not brought back).
	Update(ctx context.Context, t templatedomain.Template) (templatedomain.Template, error)
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

func (r *SQLite) Create(ctx context.Context, t templatedomain.Template) error {
	spec, _ := json.Marshal(t.Spec())
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO templates (id, workspace_id, spec, source, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID.String(), t.WorkspaceID.String(), string(spec), string(t.Source), timeutil.ToMillis(t.CreatedAt), timeutil.ToMillis(t.UpdatedAt), t.Version)
	if err != nil {
		return fmt.Errorf("create template: %w", err)
	}
	return nil
}

func (r *SQLite) Update(ctx context.Context, t templatedomain.Template) (templatedomain.Template, error) {
	spec, _ := json.Marshal(t.Spec())
	res, err := r.q.W(ctx).ExecContext(ctx, `UPDATE templates SET spec = ?, updated_at = ?, version = version + 1
		WHERE workspace_id = ? AND id = ? AND version = ?`,
		string(spec), timeutil.ToMillis(t.UpdatedAt), t.WorkspaceID.String(), t.ID.String(), t.Version)
	if err != nil {
		return t, fmt.Errorf("update template: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := r.Get(ctx, t.WorkspaceID, t.ID); err != nil {
			return t, err
		}
		return t, stale()
	}
	t.Version++
	return t, nil
}

func stale() error {
	return apperror.New(apperror.StaleVersion, "the template was changed by someone else; reload and retry")
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
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT id, workspace_id, spec, source, created_at, updated_at, version FROM templates `+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("query templates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []templatedomain.Template
	for rows.Next() {
		var (
			rawID, rawWS, spec, source string
			created, updated, version  int64
		)
		if err := rows.Scan(&rawID, &rawWS, &spec, &source, &created, &updated, &version); err != nil {
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
		t.UpdatedAt, t.Version = timeutil.FromMillis(updated), version
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

func (r *InMemory) Create(_ context.Context, t templatedomain.Template) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.rows {
		if x.ID == t.ID {
			return apperror.New(apperror.Conflict, "template exists")
		}
	}
	r.rows = append(r.rows, t)
	return nil
}

func (r *InMemory) Update(_ context.Context, t templatedomain.Template) (templatedomain.Template, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if r.rows[i].WorkspaceID == t.WorkspaceID && r.rows[i].ID == t.ID {
			if r.rows[i].Version != t.Version {
				return t, stale()
			}
			t.Version++
			r.rows[i] = t
			return t, nil
		}
	}
	return t, notFound()
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
