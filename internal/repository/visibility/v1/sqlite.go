package visibility

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	visibilitydomain "github.com/ahmethakanbesel/ekin/internal/domain/visibility/v1"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// SQLite is the production Repository.
type SQLite struct{ q db.QuerierSource }

var _ Repository = (*SQLite)(nil)

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

func nullMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: timeutil.ToMillis(t), Valid: true}
}

func nullID(id uuid.UUID) sql.NullString {
	if id == uuid.Nil {
		return sql.NullString{}
	}
	return sql.NullString{String: id.String(), Valid: true}
}

func parseID(s sql.NullString) uuid.UUID {
	if !s.Valid {
		return uuid.Nil
	}
	id, _ := uuid.Parse(s.String)
	return id
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- Profiles ----

func (r *SQLite) Profile(ctx context.Context, workspaceID uuid.UUID) (*visibilitydomain.Profile, error) {
	list, err := r.profiles(ctx, `WHERE workspace_id = ?`, workspaceID.String())
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperror.New(apperror.NotFound, "AI visibility is not set up")
	}
	return list[0], nil
}

func (r *SQLite) DueProfiles(ctx context.Context, now time.Time) ([]*visibilitydomain.Profile, error) {
	return r.profiles(ctx, `WHERE next_run_at IS NOT NULL AND next_run_at <= ? ORDER BY next_run_at`, timeutil.ToMillis(now))
}

func (r *SQLite) PutProfile(ctx context.Context, p *visibilitydomain.Profile) error {
	sn := p.Snapshot()
	raw, err := json.Marshal(sn.Settings)
	if err != nil {
		return err
	}
	_, err = r.q.W(ctx).ExecContext(ctx, `INSERT INTO visibility_profiles (workspace_id, settings, next_run_at, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (workspace_id) DO UPDATE SET settings = excluded.settings, next_run_at = excluded.next_run_at, updated_at = excluded.updated_at`,
		sn.WorkspaceID.String(), string(raw), nullMillis(sn.NextRunAt), timeutil.ToMillis(sn.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put visibility profile: %w", err)
	}
	return nil
}

func (r *SQLite) profiles(ctx context.Context, tail string, a ...any) (_ []*visibilitydomain.Profile, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT workspace_id, settings, next_run_at, updated_at FROM visibility_profiles `+tail, a...)
	if err != nil {
		return nil, fmt.Errorf("query visibility profiles: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []*visibilitydomain.Profile
	for rows.Next() {
		var (
			sn      visibilitydomain.ProfileSnapshot
			ws, raw string
			next    sql.NullInt64
			upd     int64
		)
		if err := rows.Scan(&ws, &raw, &next, &upd); err != nil {
			return nil, err
		}
		sn.WorkspaceID, _ = uuid.Parse(ws)
		if err := json.Unmarshal([]byte(raw), &sn.Settings); err != nil {
			return nil, fmt.Errorf("visibility settings: %w", err)
		}
		sn.NextRunAt, sn.UpdatedAt = timeutil.FromMillis(next.Int64), timeutil.FromMillis(upd)
		p, err := visibilitydomain.RehydrateProfile(sn)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- Prompts ----

const promptCols = `id, workspace_id, text, source, active, covered_by, covered_at, created_at, updated_at`

func (r *SQLite) Prompts(ctx context.Context, workspaceID uuid.UUID) ([]visibilitydomain.Prompt, error) {
	return r.prompts(ctx, `WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID.String())
}

func (r *SQLite) Prompt(ctx context.Context, workspaceID, id uuid.UUID) (visibilitydomain.Prompt, error) {
	list, err := r.prompts(ctx, `WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return visibilitydomain.Prompt{}, err
	}
	if len(list) == 0 {
		return visibilitydomain.Prompt{}, apperror.New(apperror.NotFound, "prompt not found")
	}
	return list[0], nil
}

func (r *SQLite) PutPrompt(ctx context.Context, p visibilitydomain.Prompt) error {
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO visibility_prompts (`+promptCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET text = excluded.text, active = excluded.active, covered_by = excluded.covered_by,
			covered_at = excluded.covered_at, updated_at = excluded.updated_at`,
		p.ID.String(), p.WorkspaceID.String(), p.Text, string(p.Source), boolInt(p.Active), nullID(p.CoveredBy), nullMillis(p.CoveredAt),
		timeutil.ToMillis(p.CreatedAt), timeutil.ToMillis(p.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put prompt: %w", err)
	}
	return nil
}

func (r *SQLite) DeletePrompt(ctx context.Context, workspaceID, id uuid.UUID) error {
	res, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM visibility_prompts WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.NotFound, "prompt not found")
	}
	return nil
}

func (r *SQLite) prompts(ctx context.Context, tail string, a ...any) (_ []visibilitydomain.Prompt, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+promptCols+` FROM visibility_prompts `+tail, a...)
	if err != nil {
		return nil, fmt.Errorf("query prompts: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []visibilitydomain.Prompt
	for rows.Next() {
		var (
			p                visibilitydomain.Prompt
			id, ws, source   string
			active           int
			covered          sql.NullString
			coveredAt        sql.NullInt64
			created, updated int64
		)
		if err := rows.Scan(&id, &ws, &p.Text, &source, &active, &covered, &coveredAt, &created, &updated); err != nil {
			return nil, err
		}
		p.ID, _ = uuid.Parse(id)
		p.WorkspaceID, _ = uuid.Parse(ws)
		p.Source, p.Active, p.CoveredBy = visibilitydomain.PromptSource(source), active == 1, parseID(covered)
		p.CoveredAt, p.CreatedAt, p.UpdatedAt = timeutil.FromMillis(coveredAt.Int64), timeutil.FromMillis(created), timeutil.FromMillis(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- Runs ----

const runCols = `id, workspace_id, status, trigger, planned, done, failed, cost, failure, created_at, started_at, finished_at`

func (r *SQLite) CreateRun(ctx context.Context, run visibilitydomain.Run) error {
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO visibility_runs (`+runCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID.String(), run.WorkspaceID.String(), string(run.Status), run.Trigger, run.Planned, run.Done, run.Failed, run.Cost, run.Failure,
		timeutil.ToMillis(run.CreatedAt), nullMillis(run.StartedAt), nullMillis(run.FinishedAt))
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

func (r *SQLite) SaveRun(ctx context.Context, run visibilitydomain.Run) error {
	_, err := r.q.W(ctx).ExecContext(ctx, `UPDATE visibility_runs SET status = ?, planned = ?, done = ?, failed = ?, cost = ?, failure = ?,
		started_at = ?, finished_at = ? WHERE id = ? AND workspace_id = ?`,
		string(run.Status), run.Planned, run.Done, run.Failed, run.Cost, run.Failure, nullMillis(run.StartedAt), nullMillis(run.FinishedAt),
		run.ID.String(), run.WorkspaceID.String())
	return err
}

func (r *SQLite) Run(ctx context.Context, workspaceID, id uuid.UUID) (visibilitydomain.Run, error) {
	list, err := r.runs(ctx, `WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return visibilitydomain.Run{}, err
	}
	if len(list) == 0 {
		return visibilitydomain.Run{}, apperror.New(apperror.NotFound, "run not found")
	}
	return list[0], nil
}

func (r *SQLite) Runs(ctx context.Context, workspaceID uuid.UUID, limit int) ([]visibilitydomain.Run, error) {
	return r.runs(ctx, `WHERE workspace_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, workspaceID.String(), limit)
}

func (r *SQLite) runs(ctx context.Context, tail string, a ...any) (_ []visibilitydomain.Run, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT `+runCols+` FROM visibility_runs `+tail, a...)
	if err != nil {
		return nil, fmt.Errorf("query runs: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []visibilitydomain.Run
	for rows.Next() {
		var (
			run               visibilitydomain.Run
			id, ws, status    string
			created           int64
			started, finished sql.NullInt64
		)
		if err := rows.Scan(&id, &ws, &status, &run.Trigger, &run.Planned, &run.Done, &run.Failed, &run.Cost, &run.Failure,
			&created, &started, &finished); err != nil {
			return nil, err
		}
		run.ID, _ = uuid.Parse(id)
		run.WorkspaceID, _ = uuid.Parse(ws)
		run.Status = visibilitydomain.RunStatus(status)
		run.CreatedAt, run.StartedAt, run.FinishedAt = timeutil.FromMillis(created), timeutil.FromMillis(started.Int64), timeutil.FromMillis(finished.Int64)
		out = append(out, run)
	}
	return out, rows.Err()
}

// ---- Answers ----

func (r *SQLite) PutAnswer(ctx context.Context, workspaceID uuid.UUID, a visibilitydomain.Answer) error {
	cites, _ := json.Marshal(nonNil(a.Citations))
	analysis, _ := json.Marshal(a.Analysis)
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO visibility_answers
		(id, workspace_id, run_id, prompt_id, model, text, citations, analysis, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID.String(), workspaceID.String(), a.RunID.String(), a.PromptID.String(), a.Model, a.Text, string(cites), string(analysis), a.Error,
		timeutil.ToMillis(a.CreatedAt))
	if err != nil {
		return fmt.Errorf("put answer: %w", err)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (r *SQLite) Answers(ctx context.Context, workspaceID, runID uuid.UUID) (_ []visibilitydomain.Answer, err error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT a.id, a.run_id, a.prompt_id, a.model, a.text, a.citations, a.analysis, a.error, a.created_at
		FROM visibility_answers a JOIN visibility_prompts p ON p.id = a.prompt_id
		WHERE a.workspace_id = ? AND a.run_id = ? ORDER BY p.created_at, p.id, a.model`, workspaceID.String(), runID.String())
	if err != nil {
		return nil, fmt.Errorf("query answers: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var out []visibilitydomain.Answer
	for rows.Next() {
		var (
			a               visibilitydomain.Answer
			id, run, prompt string
			cites, analysis string
			created         int64
		)
		if err := rows.Scan(&id, &run, &prompt, &a.Model, &a.Text, &cites, &analysis, &a.Error, &created); err != nil {
			return nil, err
		}
		a.ID, _ = uuid.Parse(id)
		a.RunID, _ = uuid.Parse(run)
		a.PromptID, _ = uuid.Parse(prompt)
		_ = json.Unmarshal([]byte(cites), &a.Citations)
		_ = json.Unmarshal([]byte(analysis), &a.Analysis)
		a.CreatedAt = timeutil.FromMillis(created)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *SQLite) CallsSince(ctx context.Context, workspaceID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := r.q.R(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM visibility_answers WHERE workspace_id = ? AND created_at >= ?`,
		workspaceID.String(), timeutil.ToMillis(since)).Scan(&n)
	return n, err
}

func (r *SQLite) ForgetTexts(ctx context.Context, cutoff time.Time) (int, error) {
	res, err := r.q.W(ctx).ExecContext(ctx, `UPDATE visibility_answers SET text = '' WHERE created_at < ? AND text != ''`, timeutil.ToMillis(cutoff))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
