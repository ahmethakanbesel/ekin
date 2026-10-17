package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	knowledgedomain "kurgu/internal/domain/knowledge/v1"
	"kurgu/pkg/fold"
	"kurgu/pkg/page"
	"kurgu/pkg/timeutil"
)

// SQLite is the production Repository.
type SQLite struct {
	q db.QuerierSource
}

var _ Repository = (*SQLite)(nil)

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

const columns = `id, workspace_id, title, filename, format, size, sha256, status, page_count, tokens, failure, created_by, created_at, updated_at`

func (r *SQLite) Create(ctx context.Context, d *knowledgedomain.Document) error {
	s := d.Snapshot()
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO documents (`+columns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID.String(), s.WorkspaceID.String(), s.Title, s.Filename, s.Format, s.Size, s.SHA256, s.Status,
		s.PageCount, s.Tokens, s.Failure, nullID(s.CreatedBy), timeutil.ToMillis(s.CreatedAt), timeutil.ToMillis(s.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func (r *SQLite) Update(ctx context.Context, d *knowledgedomain.Document) error {
	s := d.Snapshot()
	res, err := r.q.W(ctx).ExecContext(ctx,
		`UPDATE documents SET title = ?, status = ?, page_count = ?, tokens = ?, failure = ?, updated_at = ?
		  WHERE id = ? AND workspace_id = ?`,
		s.Title, s.Status, s.PageCount, s.Tokens, s.Failure, timeutil.ToMillis(s.UpdatedAt), s.ID.String(), s.WorkspaceID.String())
	if err != nil {
		return fmt.Errorf("update document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound()
	}
	return nil
}

func (r *SQLite) Get(ctx context.Context, workspaceID, id uuid.UUID) (*knowledgedomain.Document, error) {
	return r.get(ctx, `id = ? AND workspace_id = ?`, id.String(), workspaceID.String())
}

func (r *SQLite) GetByID(ctx context.Context, id uuid.UUID) (*knowledgedomain.Document, error) {
	return r.get(ctx, `id = ?`, id.String())
}

func (r *SQLite) get(ctx context.Context, where string, args ...any) (*knowledgedomain.Document, error) {
	d, err := scan(r.q.R(ctx).QueryRowContext(ctx, `SELECT `+columns+` FROM documents WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound()
	}
	return d, err
}

func (r *SQLite) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*knowledgedomain.Document, string, error) {
	where := []string{"workspace_id = ?"}
	args := []any{workspaceID.String()}
	if len(f.Statuses) > 0 {
		where = append(where, "status IN ("+placeholders(len(f.Statuses))+")")
		for _, st := range f.Statuses {
			args = append(args, string(st))
		}
	}
	if len(f.Formats) > 0 {
		where = append(where, "format IN ("+placeholders(len(f.Formats))+")")
		for _, fm := range f.Formats {
			args = append(args, string(fm))
		}
	}
	if q := fold.String(f.Query); q != "" {
		where = append(where, "instr(fold(title), ?) > 0")
		args = append(args, q)
	}
	var after cursor
	if ok, err := page.Decode(f.After, &after); err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	} else if ok {
		where = append(where, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, after.At, after.At, after.ID)
	}
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+columns+` FROM documents WHERE `+strings.Join(where, " AND ")+
		` ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, "", fmt.Errorf("list documents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*knowledgedomain.Document
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(out) <= limit {
		return out, "", nil
	}
	return out[:limit], nextCursor(out[limit-1]), nil
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func (r *SQLite) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	res, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM documents WHERE id = ? AND workspace_id = ?`, id.String(), workspaceID.String())
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound()
	}
	return nil
}

func (r *SQLite) ReplacePages(ctx context.Context, documentID uuid.UUID, pages []knowledgedomain.Page) error {
	q := r.q.W(ctx)
	if _, err := q.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id = ?`, documentID.String()); err != nil {
		return fmt.Errorf("clear pages: %w", err)
	}
	for _, p := range pages {
		if _, err := q.ExecContext(ctx,
			`INSERT INTO document_pages (document_id, number, text, words, tokens) VALUES (?, ?, ?, ?, ?)`,
			documentID.String(), p.Number, p.Text, p.Words, p.Tokens); err != nil {
			return fmt.Errorf("insert page %d: %w", p.Number, err)
		}
	}
	return nil
}

func (r *SQLite) Pages(ctx context.Context, documentID uuid.UUID, from, to int) ([]knowledgedomain.Page, error) {
	if from < 1 {
		from = 1
	}
	if to == 0 {
		to = knowledgedomain.MaxPages
	}
	rows, err := r.q.R(ctx).QueryContext(ctx,
		`SELECT number, text, words, tokens FROM document_pages
		  WHERE document_id = ? AND number BETWEEN ? AND ? ORDER BY number`, documentID.String(), from, to)
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []knowledgedomain.Page
	for rows.Next() {
		var p knowledgedomain.Page
		if err := rows.Scan(&p.Number, &p.Text, &p.Words, &p.Tokens); err != nil {
			return nil, fmt.Errorf("scan page: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SQLite) ReplaceHeadings(ctx context.Context, documentID uuid.UUID, hs []knowledgedomain.Heading) error {
	q := r.q.W(ctx)
	if _, err := q.ExecContext(ctx, `DELETE FROM document_headings WHERE document_id = ?`, documentID.String()); err != nil {
		return fmt.Errorf("clear headings: %w", err)
	}
	for i, h := range hs {
		if _, err := q.ExecContext(ctx,
			`INSERT INTO document_headings (document_id, position, level, title, page) VALUES (?, ?, ?, ?, ?)`,
			documentID.String(), i, h.Level, h.Title, h.Page); err != nil {
			return fmt.Errorf("insert heading %d: %w", i, err)
		}
	}
	return nil
}

func (r *SQLite) Headings(ctx context.Context, documentID uuid.UUID) ([]knowledgedomain.Heading, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx,
		`SELECT level, title, page FROM document_headings WHERE document_id = ? ORDER BY position`, documentID.String())
	if err != nil {
		return nil, fmt.Errorf("list headings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []knowledgedomain.Heading
	for rows.Next() {
		var h knowledgedomain.Heading
		if err := rows.Scan(&h.Level, &h.Title, &h.Page); err != nil {
			return nil, fmt.Errorf("scan heading: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scan(s scanner) (*knowledgedomain.Document, error) {
	var (
		snap                knowledgedomain.Snapshot
		rawID, rawWS        string
		createdBy           sql.NullString
		createdAt, updateAt int64
	)
	err := s.Scan(&rawID, &rawWS, &snap.Title, &snap.Filename, &snap.Format, &snap.Size, &snap.SHA256, &snap.Status,
		&snap.PageCount, &snap.Tokens, &snap.Failure, &createdBy, &createdAt, &updateAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan document: %w", err)
	}
	if snap.ID, err = uuid.Parse(rawID); err != nil {
		return nil, fmt.Errorf("parse document id: %w", err)
	}
	if snap.WorkspaceID, err = uuid.Parse(rawWS); err != nil {
		return nil, fmt.Errorf("parse document workspace: %w", err)
	}
	if createdBy.Valid {
		snap.CreatedBy, _ = uuid.Parse(createdBy.String)
	}
	snap.CreatedAt, snap.UpdatedAt = timeutil.FromMillis(createdAt), timeutil.FromMillis(updateAt)
	return knowledgedomain.Rehydrate(snap)
}

func nullID(id uuid.UUID) sql.NullString {
	if id == uuid.Nil {
		return sql.NullString{}
	}
	return sql.NullString{String: id.String(), Valid: true}
}

func notFound() error { return apperror.New(apperror.DocumentNotFound, "document not found") }
