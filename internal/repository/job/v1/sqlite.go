package job

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	"github.com/ahmethakanbesel/ekin/pkg/page"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
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

const jobColumns = `id, workspace_id, trace_id, queue, kind, payload, unique_key, status,
	attempt, max_attempts, run_after, lease_owner, lease_until, last_error,
	created_at, updated_at, started_at, finished_at, concurrency_key`

func (r *SQLite) Enqueue(ctx context.Context, j *jobdomain.Job) (bool, error) {
	s := j.Snapshot()
	res, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO jobs (`+jobColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (unique_key) WHERE unique_key IS NOT NULL DO NOTHING`,
		s.ID.String(), s.WorkspaceID.String(), s.TraceID.String(), s.Queue, s.Kind, string(s.Payload), nullString(s.UniqueKey), s.Status,
		s.Attempt, s.MaxAttempts, timeutil.ToMillis(s.RunAfter), nullString(s.LeaseOwner), nullMillis(s.LeaseUntil), s.LastError,
		timeutil.ToMillis(s.CreatedAt), timeutil.ToMillis(s.UpdatedAt), nullMillis(s.StartedAt), nullMillis(s.FinishedAt),
		nullString(s.ConcurrencyKey))
	if err != nil {
		return false, fmt.Errorf("insert job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("insert job: %w", err)
	}
	return n == 1, nil
}

func (r *SQLite) Save(ctx context.Context, j *jobdomain.Job) error {
	s := j.Snapshot()
	res, err := r.q.W(ctx).ExecContext(ctx, `UPDATE jobs SET
			status = ?, attempt = ?, run_after = ?, lease_owner = ?, lease_until = ?,
			last_error = ?, updated_at = ?, started_at = ?, finished_at = ?
		WHERE id = ?`,
		s.Status, s.Attempt, timeutil.ToMillis(s.RunAfter), nullString(s.LeaseOwner), nullMillis(s.LeaseUntil),
		s.LastError, timeutil.ToMillis(s.UpdatedAt), nullMillis(s.StartedAt), nullMillis(s.FinishedAt), s.ID.String())
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.JobNotFound, "job not found")
	}
	return nil
}

func (r *SQLite) Get(ctx context.Context, workspaceID, id uuid.UUID) (*jobdomain.Job, error) {
	return r.one(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ? AND workspace_id = ?`, id.String(), workspaceID.String())
}

func (r *SQLite) GetByID(ctx context.Context, id uuid.UUID) (*jobdomain.Job, error) {
	return r.one(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id.String())
}

func (r *SQLite) one(ctx context.Context, query string, args ...any) (*jobdomain.Job, error) {
	j, err := scanJob(r.q.R(ctx).QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.New(apperror.JobNotFound, "job not found")
	}
	return j, err
}

func (r *SQLite) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*jobdomain.Job, string, error) {
	where := `workspace_id = ?`
	args := []any{workspaceID.String()}
	if len(f.Statuses) > 0 {
		where += ` AND status IN (` + placeholders(len(f.Statuses)) + `)`
		for _, s := range f.Statuses {
			args = append(args, string(s))
		}
	}
	if len(f.Kinds) > 0 {
		where += ` AND kind IN (` + placeholders(len(f.Kinds)) + `)`
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	having := ``
	var after cursor
	if ok, err := page.Decode(f.After, &after); err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	} else if ok {
		having = ` HAVING MAX(created_at) < ? OR (MAX(created_at) = ? AND trace_id < ?)`
		args = append(args, after.At, after.At, after.Trace)
	}
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	args = append(args, limit+1)
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT trace_id, MAX(created_at) FROM jobs WHERE `+where+
		` GROUP BY trace_id`+having+` ORDER BY MAX(created_at) DESC, trace_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query job traces: %w", err)
	}
	var traces []cursor
	for rows.Next() {
		var c cursor
		if err := rows.Scan(&c.Trace, &c.At); err != nil {
			_ = rows.Close()
			return nil, "", fmt.Errorf("scan job trace: %w", err)
		}
		traces = append(traces, c)
	}
	if err := rows.Close(); err != nil {
		return nil, "", err
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(traces) > limit {
		traces = traces[:limit]
		next = page.Encode(traces[limit-1])
	}
	if len(traces) == 0 {
		return nil, "", nil
	}
	jobArgs := []any{workspaceID.String()}
	for _, c := range traces {
		jobArgs = append(jobArgs, c.Trace)
	}
	jobs, err := r.many(ctx, `SELECT `+jobColumns+` FROM jobs
		WHERE workspace_id = ? AND trace_id IN (`+placeholders(len(traces))+`)
		ORDER BY created_at DESC, id DESC`, jobArgs...)
	return jobs, next, err
}

func (r *SQLite) NextClaimable(ctx context.Context, queues []jobdomain.Queue, kinds []string, now time.Time) (*jobdomain.Job, error) {
	if len(queues) == 0 || len(kinds) == 0 {
		return nil, nil
	}
	ms := timeutil.ToMillis(now)
	args := make([]any, 0, len(queues)+len(kinds)+3)
	for _, q := range queues {
		args = append(args, string(q))
	}
	for _, k := range kinds {
		args = append(args, k)
	}
	args = append(args, ms, ms, ms, 1)
	// Mirrors jobdomain.Job.Claimable and Holds; the caller still asks
	// the aggregate before starting it.
	jobs, err := r.many(ctx, `SELECT `+jobColumns+` FROM jobs
		WHERE queue IN (`+placeholders(len(queues))+`)
		  AND kind IN (`+placeholders(len(kinds))+`)
		  AND ((status = 'queued' AND run_after <= ?)
		    OR (status = 'running' AND lease_until < ? AND attempt < max_attempts))
		  AND (concurrency_key IS NULL OR NOT EXISTS (
		    SELECT 1 FROM jobs holder WHERE holder.concurrency_key = jobs.concurrency_key AND holder.id <> jobs.id
		      AND holder.status = 'running' AND holder.lease_until >= ?))
		ORDER BY run_after, created_at, id
		LIMIT ?`, args...)
	if err != nil || len(jobs) == 0 {
		return nil, err
	}
	return jobs[0], nil
}

func (r *SQLite) ListAbandoned(ctx context.Context, now time.Time, limit int) ([]*jobdomain.Job, error) {
	return r.many(ctx, `SELECT `+jobColumns+` FROM jobs
		WHERE status = 'running' AND lease_until < ? AND attempt >= max_attempts
		ORDER BY lease_until, id LIMIT ?`, timeutil.ToMillis(now), limit)
}

func (r *SQLite) many(ctx context.Context, query string, args ...any) ([]*jobdomain.Job, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*jobdomain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(s scanner) (*jobdomain.Job, error) {
	var (
		id, workspaceID, traceID, payload  string
		uniqueKey, leaseOwner, concurrency sql.NullString
		runAfter, createdAt, updatedAt     int64
		leaseUntil, startedAt, finishedAt  sql.NullInt64
		snap                               jobdomain.Snapshot
	)
	err := s.Scan(&id, &workspaceID, &traceID, &snap.Queue, &snap.Kind, &payload, &uniqueKey, &snap.Status,
		&snap.Attempt, &snap.MaxAttempts, &runAfter, &leaseOwner, &leaseUntil, &snap.LastError,
		&createdAt, &updatedAt, &startedAt, &finishedAt, &concurrency)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan job: %w", err)
	}
	if snap.ID, err = uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("parse job id: %w", err)
	}
	if snap.WorkspaceID, err = uuid.Parse(workspaceID); err != nil {
		return nil, fmt.Errorf("parse workspace id: %w", err)
	}
	if traceID != "" {
		if snap.TraceID, err = uuid.Parse(traceID); err != nil {
			return nil, fmt.Errorf("parse trace id: %w", err)
		}
	}
	snap.Payload = []byte(payload)
	snap.UniqueKey = uniqueKey.String
	snap.ConcurrencyKey = concurrency.String
	snap.LeaseOwner = leaseOwner.String
	snap.RunAfter = timeutil.FromMillis(runAfter)
	snap.LeaseUntil = timeutil.FromMillis(leaseUntil.Int64)
	snap.CreatedAt = timeutil.FromMillis(createdAt)
	snap.UpdatedAt = timeutil.FromMillis(updatedAt)
	snap.StartedAt = timeutil.FromMillis(startedAt.Int64)
	snap.FinishedAt = timeutil.FromMillis(finishedAt.Int64)
	return jobdomain.Rehydrate(snap)
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: timeutil.ToMillis(t), Valid: true}
}
