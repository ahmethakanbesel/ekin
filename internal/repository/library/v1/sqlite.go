package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/internal/db"
	librarydomain "ekin/internal/domain/library/v1"
	"ekin/pkg/fold"
	"ekin/pkg/page"
	"ekin/pkg/timeutil"
)

// SQLite is the production Repository.
type SQLite struct {
	q db.QuerierSource
}

var _ Repository = (*SQLite)(nil)

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

const columns = `id, workspace_id, kind, origin, status, title, description, tags, mood, tempo, credit, filename, size, sha256,
	format, width, height, orientation, duration_ms, poster, failure, created_by, created_at, updated_at, archived_at,
	provisional, kept, harvest_story, harvest_deliverable, harvest_revision, source_ref, source_url, original_url, derived_from`

func (r *SQLite) Create(ctx context.Context, a *librarydomain.Asset) error {
	args, err := values(a)
	if err != nil {
		return err
	}
	_, err = r.q.W(ctx).ExecContext(ctx, `INSERT INTO library_assets (`+columns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...)
	if db.IsUniqueViolation(err) {
		return duplicate()
	}
	if err != nil {
		return fmt.Errorf("insert asset: %w", err)
	}
	return nil
}

func (r *SQLite) Update(ctx context.Context, a *librarydomain.Asset) error {
	s := a.Snapshot()
	tags, credit, err := encode(s)
	if err != nil {
		return err
	}
	res, err := r.q.W(ctx).ExecContext(ctx,
		`UPDATE library_assets SET status = ?, title = ?, description = ?, tags = ?, mood = ?, tempo = ?, credit = ?,
		        format = ?, width = ?, height = ?, orientation = ?, duration_ms = ?, poster = ?, failure = ?,
		        updated_at = ?, archived_at = ?, provisional = ?, kept = ?
		  WHERE id = ? AND workspace_id = ?`,
		s.Status, s.Title, s.Description, tags, s.Mood, s.Tempo, credit,
		s.Format, s.Width, s.Height, string(a.Orientation()), s.Duration.Milliseconds(), s.Poster, s.Failure,
		timeutil.ToMillis(s.UpdatedAt), nullTime(s.ArchivedAt), s.Provisional, s.Kept, s.ID.String(), s.WorkspaceID.String())
	if err != nil {
		return fmt.Errorf("update asset: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound()
	}
	return nil
}

func (r *SQLite) Get(ctx context.Context, workspaceID, id uuid.UUID) (*librarydomain.Asset, error) {
	return r.get(ctx, `id = ? AND workspace_id = ?`, id.String(), workspaceID.String())
}

func (r *SQLite) GetByID(ctx context.Context, id uuid.UUID) (*librarydomain.Asset, error) {
	return r.get(ctx, `id = ?`, id.String())
}

func (r *SQLite) FindBySourceRef(ctx context.Context, workspaceID uuid.UUID, ref string) (*librarydomain.Asset, error) {
	return r.get(ctx, `workspace_id = ? AND source_ref = ?`, workspaceID.String(), ref)
}

func (r *SQLite) Provisional(ctx context.Context, workspaceID, deliverableID uuid.UUID) ([]*librarydomain.Asset, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+columns+` FROM library_assets
		WHERE workspace_id = ? AND harvest_deliverable = ? AND provisional = 1 ORDER BY created_at, id`,
		workspaceID.String(), deliverableID.String())
	if err != nil {
		return nil, fmt.Errorf("list provisional assets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*librarydomain.Asset
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *SQLite) GetCollect(ctx context.Context, workspaceID uuid.UUID) (librarydomain.Collect, error) {
	var c librarydomain.Collect
	err := r.q.R(ctx).QueryRowContext(ctx, `SELECT music, video, photo FROM library_settings WHERE workspace_id = ?`,
		workspaceID.String()).Scan(&c.Music, &c.Video, &c.Photo)
	if errors.Is(err, sql.ErrNoRows) {
		return librarydomain.Collect{}, nil
	}
	if err != nil {
		return c, fmt.Errorf("get library settings: %w", err)
	}
	return c, nil
}

func (r *SQLite) SaveCollect(ctx context.Context, workspaceID uuid.UUID, c librarydomain.Collect, now time.Time) error {
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO library_settings (workspace_id, music, video, photo, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (workspace_id) DO UPDATE SET music = excluded.music, video = excluded.video, photo = excluded.photo,
		updated_at = excluded.updated_at`,
		workspaceID.String(), c.Music, c.Video, c.Photo, timeutil.ToMillis(now))
	if err != nil {
		return fmt.Errorf("save library settings: %w", err)
	}
	return nil
}

func (r *SQLite) FindBySHA256(ctx context.Context, workspaceID uuid.UUID, sha256 string) (*librarydomain.Asset, error) {
	return r.get(ctx, `workspace_id = ? AND sha256 = ?`, workspaceID.String(), sha256)
}

func (r *SQLite) get(ctx context.Context, where string, args ...any) (*librarydomain.Asset, error) {
	a, err := scan(r.q.R(ctx).QueryRowContext(ctx, `SELECT `+columns+` FROM library_assets WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound()
	}
	return a, err
}

func (r *SQLite) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*librarydomain.Asset, string, error) {
	where := []string{"workspace_id = ?"}
	args := []any{workspaceID.String()}
	in := func(column string, vals []string) {
		if len(vals) == 0 {
			return
		}
		where = append(where, column+" IN ("+placeholders(len(vals))+")")
		for _, v := range vals {
			args = append(args, v)
		}
	}
	in("kind", strs(f.Kinds))
	in("origin", strs(f.Origins))
	in("status", strs(f.Statuses))
	in("orientation", strs(f.Orientations))
	if !f.Archived {
		where = append(where, "archived_at IS NULL")
	}
	if q := fold.String(f.Query); q != "" {
		where = append(where, "instr(fold(title || ' ' || tags || ' ' || description), ?) > 0")
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
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+columns+` FROM library_assets WHERE `+strings.Join(where, " AND ")+
		` ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, "", fmt.Errorf("list assets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*librarydomain.Asset
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(out) <= limit {
		return out, "", nil
	}
	return out[:limit], nextCursor(out[limit-1]), nil
}

func (r *SQLite) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	res, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM library_assets WHERE id = ? AND workspace_id = ?`, id.String(), workspaceID.String())
	if err != nil {
		return fmt.Errorf("delete asset: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound()
	}
	return nil
}

func (r *SQLite) RecordUsage(ctx context.Context, u librarydomain.Usage) error {
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO library_usages (asset_id, workspace_id, story_id, deliverable_id, revision, used_at)
		SELECT id, workspace_id, ?, ?, ?, ? FROM library_assets WHERE id = ? AND workspace_id = ?
		ON CONFLICT DO NOTHING`,
		u.StoryID.String(), u.DeliverableID.String(), u.Revision, timeutil.ToMillis(u.At), u.AssetID.String(), u.WorkspaceID.String())
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}

func (r *SQLite) Usages(ctx context.Context, workspaceID, assetID uuid.UUID) ([]librarydomain.Usage, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT story_id, deliverable_id, revision, used_at FROM library_usages
		WHERE workspace_id = ? AND asset_id = ? ORDER BY used_at DESC, deliverable_id DESC, revision DESC`,
		workspaceID.String(), assetID.String())
	if err != nil {
		return nil, fmt.Errorf("list usages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []librarydomain.Usage
	for rows.Next() {
		var (
			story, deliverable string
			at                 int64
		)
		u := librarydomain.Usage{AssetID: assetID, WorkspaceID: workspaceID}
		if err := rows.Scan(&story, &deliverable, &u.Revision, &at); err != nil {
			return nil, fmt.Errorf("scan usage: %w", err)
		}
		u.StoryID, _ = uuid.Parse(story)
		u.DeliverableID, _ = uuid.Parse(deliverable)
		u.At = timeutil.FromMillis(at)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *SQLite) UsageCounts(ctx context.Context, workspaceID uuid.UUID, assetIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	args := []any{workspaceID.String()}
	for _, id := range assetIDs {
		args = append(args, id.String())
	}
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT asset_id, COUNT(DISTINCT deliverable_id) FROM library_usages
		WHERE workspace_id = ? AND asset_id IN (`+placeholders(len(assetIDs))+`) GROUP BY asset_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("count usages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			raw string
			n   int
		)
		if err := rows.Scan(&raw, &n); err != nil {
			return nil, fmt.Errorf("scan usage count: %w", err)
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse usage asset: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

func strs[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func encode(s librarydomain.Snapshot) (string, string, error) {
	tags, err := json.Marshal(s.Tags)
	if err != nil {
		return "", "", err
	}
	credit, err := json.Marshal(s.Credit)
	if err != nil {
		return "", "", err
	}
	return string(tags), string(credit), nil
}

func values(a *librarydomain.Asset) ([]any, error) {
	s := a.Snapshot()
	tags, credit, err := encode(s)
	if err != nil {
		return nil, err
	}
	return []any{
		s.ID.String(), s.WorkspaceID.String(), s.Kind, s.Origin, s.Status, s.Title, s.Description, tags, s.Mood, s.Tempo,
		credit, s.Filename, s.Size, s.SHA256, s.Format, s.Width, s.Height, string(a.Orientation()), s.Duration.Milliseconds(),
		s.Poster, s.Failure, nullID(s.CreatedBy), timeutil.ToMillis(s.CreatedAt), timeutil.ToMillis(s.UpdatedAt),
		nullTime(s.ArchivedAt),
		s.Provisional, s.Kept, nullID(s.Harvest.StoryID), nullID(s.Harvest.DeliverableID), s.Harvest.Revision,
		nullString(s.Harvest.SourceRef), s.Harvest.SourceURL, s.Harvest.OriginalURL, nullID(s.Harvest.DerivedFrom),
	}, nil
}

func nullString(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }

type scanner interface{ Scan(dest ...any) error }

func scan(s scanner) (*librarydomain.Asset, error) {
	var (
		snap                       librarydomain.Snapshot
		rawID, rawWS, tags, credit string
		orientation                string
		durationMS                 int64
		createdBy                  sql.NullString
		createdAt, updatedAt       int64
		archivedAt                 sql.NullInt64
		story, deliverable         sql.NullString
		sourceRef, derived         sql.NullString
	)
	err := s.Scan(&rawID, &rawWS, &snap.Kind, &snap.Origin, &snap.Status, &snap.Title, &snap.Description, &tags, &snap.Mood,
		&snap.Tempo, &credit, &snap.Filename, &snap.Size, &snap.SHA256, &snap.Format, &snap.Width, &snap.Height, &orientation,
		&durationMS, &snap.Poster, &snap.Failure, &createdBy, &createdAt, &updatedAt, &archivedAt,
		&snap.Provisional, &snap.Kept, &story, &deliverable, &snap.Harvest.Revision, &sourceRef, &snap.Harvest.SourceURL,
		&snap.Harvest.OriginalURL, &derived)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan asset: %w", err)
	}
	if snap.ID, err = uuid.Parse(rawID); err != nil {
		return nil, fmt.Errorf("parse asset id: %w", err)
	}
	if snap.WorkspaceID, err = uuid.Parse(rawWS); err != nil {
		return nil, fmt.Errorf("parse asset workspace: %w", err)
	}
	if err := json.Unmarshal([]byte(tags), &snap.Tags); err != nil {
		return nil, fmt.Errorf("parse asset tags: %w", err)
	}
	if err := json.Unmarshal([]byte(credit), &snap.Credit); err != nil {
		return nil, fmt.Errorf("parse asset credit: %w", err)
	}
	if createdBy.Valid {
		snap.CreatedBy, _ = uuid.Parse(createdBy.String)
	}
	snap.Duration = time.Duration(durationMS) * time.Millisecond
	snap.CreatedAt, snap.UpdatedAt = timeutil.FromMillis(createdAt), timeutil.FromMillis(updatedAt)
	if archivedAt.Valid {
		snap.ArchivedAt = timeutil.FromMillis(archivedAt.Int64)
	}
	snap.Harvest.StoryID, _ = uuid.Parse(story.String)
	snap.Harvest.DeliverableID, _ = uuid.Parse(deliverable.String)
	snap.Harvest.DerivedFrom, _ = uuid.Parse(derived.String)
	snap.Harvest.SourceRef = sourceRef.String
	return librarydomain.Rehydrate(snap)
}

func nullID(id uuid.UUID) sql.NullString {
	if id == uuid.Nil {
		return sql.NullString{}
	}
	return sql.NullString{String: id.String(), Valid: true}
}

func nullTime(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: timeutil.ToMillis(t), Valid: true}
}

func notFound() error { return apperror.New(apperror.AssetNotFound, "asset not found") }

func duplicate() error {
	return apperror.New(apperror.Conflict, "the library already holds this file")
}
