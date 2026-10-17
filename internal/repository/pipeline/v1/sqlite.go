package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	voicedomain "kurgu/internal/domain/voice/v1"
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

const pipelineColumns = `id, workspace_id, name, language, outputs, source,
	schedule_cron, schedule_tz, schedule_batch, status, next_run_at, last_run_at,
	created_at, updated_at, version, template_id, voice, self_review, production`

// outputsJSON is the stored shape of the outputs column.
type outputsJSON struct {
	Video   *pipelinedomain.VideoSnapshot   `json:"video"`
	Article *pipelinedomain.ArticleSnapshot `json:"article"`
	Lead    string                          `json:"lead"`
}

// row is a Snapshot flattened into column values.
type row struct {
	id, workspaceID, name, language, outputs, source string
	cron, tz                                         sql.NullString
	batch                                            sql.NullInt64
	status                                           string
	nextRunAt, lastRunAt                             sql.NullInt64
	createdAt, updatedAt, version                    int64
	templateID                                       sql.NullString
	voice                                            sql.NullString
	selfReview                                       int64
	production                                       sql.NullString
}

func toRow(s pipelinedomain.Snapshot) (row, error) {
	outputs, err := json.Marshal(outputsJSON{Video: s.Video, Article: s.Article, Lead: s.Lead})
	if err != nil {
		return row{}, fmt.Errorf("encode outputs: %w", err)
	}
	source, err := json.Marshal(s.Source)
	if err != nil {
		return row{}, fmt.Errorf("encode source: %w", err)
	}
	r := row{
		id:          s.ID.String(),
		workspaceID: s.WorkspaceID.String(),
		name:        s.Name,
		language:    s.Language,
		outputs:     string(outputs),
		source:      string(source),
		status:      s.Status,
		nextRunAt:   nullMillis(s.NextRunAt),
		lastRunAt:   nullMillis(s.LastRunAt),
		createdAt:   timeutil.ToMillis(s.CreatedAt),
		updatedAt:   timeutil.ToMillis(s.UpdatedAt),
		version:     s.Version,
		selfReview:  int64(s.SelfReview),
	}
	if s.TemplateID != uuid.Nil {
		r.templateID = sql.NullString{String: s.TemplateID.String(), Valid: true}
	}
	if s.Voice != nil {
		raw, _ := json.Marshal(s.Voice)
		r.voice = sql.NullString{String: string(raw), Valid: true}
	}
	if s.Production != (pipelinedomain.Production{}) {
		raw, _ := json.Marshal(s.Production)
		r.production = sql.NullString{String: string(raw), Valid: true}
	}
	if sc := s.Schedule; sc != nil {
		r.cron = sql.NullString{String: sc.Cron, Valid: true}
		r.tz = sql.NullString{String: sc.Timezone, Valid: true}
		r.batch = sql.NullInt64{Int64: int64(sc.Batch), Valid: true}
	}
	return r, nil
}

func (r *SQLite) Create(ctx context.Context, p *pipelinedomain.Pipeline) error {
	v, err := toRow(p.Snapshot())
	if err != nil {
		return err
	}
	_, err = r.q.W(ctx).ExecContext(ctx, `INSERT INTO pipelines (`+pipelineColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.id, v.workspaceID, v.name, v.language, v.outputs, v.source,
		v.cron, v.tz, v.batch, v.status, v.nextRunAt, v.lastRunAt,
		v.createdAt, v.updatedAt, v.version, v.templateID, v.voice, v.selfReview, v.production)
	if err != nil {
		return fmt.Errorf("insert pipeline: %w", err)
	}
	return nil
}

func (r *SQLite) Update(ctx context.Context, p *pipelinedomain.Pipeline) (*pipelinedomain.Pipeline, error) {
	snap := p.Snapshot()
	v, err := toRow(snap)
	if err != nil {
		return nil, err
	}
	res, err := r.q.W(ctx).ExecContext(ctx, `UPDATE pipelines SET
			name = ?, language = ?, outputs = ?, source = ?,
			schedule_cron = ?, schedule_tz = ?, schedule_batch = ?, status = ?,
			next_run_at = ?, last_run_at = ?, updated_at = ?, template_id = ?, voice = ?, self_review = ?, production = ?, version = version + 1
		WHERE id = ? AND workspace_id = ? AND version = ?`,
		v.name, v.language, v.outputs, v.source,
		v.cron, v.tz, v.batch, v.status,
		v.nextRunAt, v.lastRunAt, v.updatedAt, v.templateID, v.voice, v.selfReview, v.production,
		v.id, v.workspaceID, v.version)
	if err != nil {
		return nil, fmt.Errorf("update pipeline: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := r.Get(ctx, snap.WorkspaceID, snap.ID); err != nil {
			return nil, err
		}
		return nil, apperror.New(apperror.StaleVersion, "pipeline was changed by someone else; reload and retry")
	}
	snap.Version++
	return pipelinedomain.Rehydrate(snap)
}

func (r *SQLite) Get(ctx context.Context, workspaceID, id uuid.UUID) (*pipelinedomain.Pipeline, error) {
	got := r.q.R(ctx).QueryRowContext(ctx, `SELECT `+pipelineColumns+` FROM pipelines
		WHERE id = ? AND workspace_id = ?`, id.String(), workspaceID.String())
	p, err := scanPipeline(got)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.New(apperror.PipelineNotFound, "pipeline not found")
	}
	return p, err
}

func (r *SQLite) List(ctx context.Context, workspaceID uuid.UUID) ([]*pipelinedomain.Pipeline, error) {
	return r.query(ctx, `SELECT `+pipelineColumns+` FROM pipelines
		WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID.String())
}

func (r *SQLite) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	res, err := r.q.W(ctx).ExecContext(ctx,
		`DELETE FROM pipelines WHERE id = ? AND workspace_id = ?`, id.String(), workspaceID.String())
	if err != nil {
		return fmt.Errorf("delete pipeline: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.PipelineNotFound, "pipeline not found")
	}
	return nil
}

func (r *SQLite) ListDue(ctx context.Context, now time.Time, limit int) ([]*pipelinedomain.Pipeline, error) {
	return r.query(ctx, `SELECT `+pipelineColumns+` FROM pipelines
		WHERE next_run_at IS NOT NULL AND next_run_at <= ? AND status = ?
		ORDER BY next_run_at, id LIMIT ?`,
		timeutil.ToMillis(now), string(pipelinedomain.StatusActive), limit)
}

func (r *SQLite) query(ctx context.Context, query string, args ...any) ([]*pipelinedomain.Pipeline, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query pipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*pipelinedomain.Pipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPipeline(s scanner) (*pipelinedomain.Pipeline, error) {
	var v row
	err := s.Scan(&v.id, &v.workspaceID, &v.name, &v.language, &v.outputs, &v.source,
		&v.cron, &v.tz, &v.batch, &v.status, &v.nextRunAt, &v.lastRunAt,
		&v.createdAt, &v.updatedAt, &v.version, &v.templateID, &v.voice, &v.selfReview, &v.production)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan pipeline: %w", err)
	}
	snap, err := fromRow(v)
	if err != nil {
		return nil, err
	}
	return pipelinedomain.Rehydrate(snap)
}

func fromRow(v row) (pipelinedomain.Snapshot, error) {
	id, err := uuid.Parse(v.id)
	if err != nil {
		return pipelinedomain.Snapshot{}, fmt.Errorf("parse pipeline id: %w", err)
	}
	workspaceID, err := uuid.Parse(v.workspaceID)
	if err != nil {
		return pipelinedomain.Snapshot{}, fmt.Errorf("parse workspace id: %w", err)
	}
	var outputs outputsJSON
	if err := json.Unmarshal([]byte(v.outputs), &outputs); err != nil {
		return pipelinedomain.Snapshot{}, fmt.Errorf("decode outputs: %w", err)
	}
	var source pipelinedomain.SourceSnapshot
	if err := json.Unmarshal([]byte(v.source), &source); err != nil {
		return pipelinedomain.Snapshot{}, fmt.Errorf("decode source: %w", err)
	}
	snap := pipelinedomain.Snapshot{
		ID:          id,
		WorkspaceID: workspaceID,
		Name:        v.name,
		Language:    v.language,
		Video:       outputs.Video,
		Article:     outputs.Article,
		Lead:        outputs.Lead,
		Source:      source,
		Status:      v.status,
		NextRunAt:   timeutil.FromMillis(v.nextRunAt.Int64),
		LastRunAt:   timeutil.FromMillis(v.lastRunAt.Int64),
		CreatedAt:   timeutil.FromMillis(v.createdAt),
		UpdatedAt:   timeutil.FromMillis(v.updatedAt),
		TemplateID:  parseTemplateID(v.templateID),
		Voice:       parseVoice(v.voice),
		SelfReview:  int(v.selfReview),
		Production:  parseProduction(v.production),
		Version:     v.version,
	}
	if v.cron.Valid {
		snap.Schedule = &pipelinedomain.ScheduleSnapshot{Cron: v.cron.String, Timezone: v.tz.String, Batch: int(v.batch.Int64)}
	}
	return snap, nil
}

func nullMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: timeutil.ToMillis(t), Valid: true}
}

func parseTemplateID(s sql.NullString) uuid.UUID {
	if !s.Valid {
		return uuid.Nil
	}
	id, _ := uuid.Parse(s.String)
	return id
}

// parseProduction reads the production column; NULL or unreadable JSON
// takes every default, and Rehydrate checks what was read.
func parseProduction(s sql.NullString) pipelinedomain.Production {
	var p pipelinedomain.Production
	if s.Valid {
		_ = json.Unmarshal([]byte(s.String), &p)
	}
	return p
}

func parseVoice(s sql.NullString) *voicedomain.Override {
	if !s.Valid {
		return nil
	}
	var o voicedomain.Override
	if json.Unmarshal([]byte(s.String), &o) != nil {
		return nil
	}
	return &o
}
