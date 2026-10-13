package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	"kurgu/pkg/timeutil"
)

// SQLite is the production Repository.
type SQLite struct {
	q db.QuerierSource
}

var _ Repository = (*SQLite)(nil)

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite {
	return &SQLite{q: q}
}

const workspaceColumns = `id, name, timezone, created_at, updated_at`

func (r *SQLite) Create(ctx context.Context, w *workspacedomain.Workspace) error {
	s := w.Snapshot()
	_, err := r.q.W(ctx).ExecContext(ctx,
		`INSERT INTO workspaces (`+workspaceColumns+`) VALUES (?, ?, ?, ?, ?)`,
		s.ID.String(), s.Name, s.Timezone, timeutil.ToMillis(s.CreatedAt), timeutil.ToMillis(s.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert workspace: %w", err)
	}
	return nil
}

func (r *SQLite) Update(ctx context.Context, w *workspacedomain.Workspace) error {
	s := w.Snapshot()
	res, err := r.q.W(ctx).ExecContext(ctx,
		`UPDATE workspaces SET name = ?, timezone = ?, updated_at = ? WHERE id = ?`,
		s.Name, s.Timezone, timeutil.ToMillis(s.UpdatedAt), s.ID.String())
	if err != nil {
		return fmt.Errorf("update workspace: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.WorkspaceNotFound, "workspace not found")
	}
	return nil
}

func (r *SQLite) Get(ctx context.Context, id uuid.UUID) (*workspacedomain.Workspace, error) {
	row := r.q.R(ctx).QueryRowContext(ctx,
		`SELECT `+workspaceColumns+` FROM workspaces WHERE id = ?`, id.String())
	w, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.New(apperror.WorkspaceNotFound, "workspace not found")
	}
	return w, err
}

func (r *SQLite) List(ctx context.Context) ([]*workspacedomain.Workspace, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx,
		`SELECT `+workspaceColumns+` FROM workspaces ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*workspacedomain.Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanWorkspace(s scanner) (*workspacedomain.Workspace, error) {
	var (
		rawID              string
		snap               workspacedomain.Snapshot
		createdAt, updated int64
	)
	if err := s.Scan(&rawID, &snap.Name, &snap.Timezone, &createdAt, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan workspace: %w", err)
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, fmt.Errorf("parse workspace id: %w", err)
	}
	snap.ID = id
	snap.CreatedAt = timeutil.FromMillis(createdAt)
	snap.UpdatedAt = timeutil.FromMillis(updated)
	return workspacedomain.Rehydrate(snap)
}
