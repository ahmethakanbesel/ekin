package story

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
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

const storyColumns = `id, workspace_id, pipeline_id, trace_id, origin, language, title, angle, keywords, fingerprint, brief, status, created_by, created_at, updated_at, version`

func (r *SQLite) Create(ctx context.Context, s *storydomain.Story) error {
	snap := s.Snapshot()
	keywords, _ := json.Marshal(nonNil(snap.Idea.Keywords))
	brief, _ := json.Marshal(snap.Brief)
	q := r.q.W(ctx)
	_, err := q.ExecContext(ctx, `INSERT INTO stories (`+storyColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.ID.String(), snap.WorkspaceID.String(), nullID(snap.PipelineID), nullID(snap.TraceID), snap.Origin, snap.Language,
		snap.Idea.Title, snap.Idea.Angle, string(keywords), s.Idea().Fingerprint(), string(brief), string(s.Status()),
		nullID(snap.CreatedBy), timeutil.ToMillis(snap.CreatedAt), timeutil.ToMillis(snap.UpdatedAt), snap.Version)
	if err != nil {
		return fmt.Errorf("insert story: %w", err)
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO story_search (story_id, workspace_id, text) VALUES (?, ?, ?)`,
		snap.ID.String(), snap.WorkspaceID.String(), s.SearchText()); err != nil {
		return fmt.Errorf("index story: %w", err)
	}
	return r.writeDeliverables(ctx, snap)
}

func (r *SQLite) Save(ctx context.Context, s *storydomain.Story) error {
	snap := s.Snapshot()
	res, err := r.q.W(ctx).ExecContext(ctx,
		`UPDATE stories SET status = ?, updated_at = ?, version = version + 1 WHERE id = ? AND version = ?`,
		string(s.Status()), timeutil.ToMillis(snap.UpdatedAt), snap.ID.String(), snap.Version)
	if err != nil {
		return fmt.Errorf("update story: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := r.GetByID(ctx, snap.ID); err != nil {
			return err
		}
		return apperror.New(apperror.StaleVersion, "the story changed meanwhile; reload and try again")
	}
	if err := r.writeDeliverables(ctx, snap); err != nil {
		return err
	}
	s.Bump()
	return nil
}

func (r *SQLite) writeDeliverables(ctx context.Context, snap storydomain.Snapshot) error {
	q := r.q.W(ctx)
	for i, d := range snap.Deliverables {
		video, article, pending, published := jsonOrNull(d.Video), jsonOrNull(d.Article), jsonOrNull(d.Pending), jsonOrNull(d.Published)
		var approvedAt, scriptApprovedAt sql.NullInt64
		if !d.ApprovedAt.IsZero() {
			approvedAt = sql.NullInt64{Int64: timeutil.ToMillis(d.ApprovedAt), Valid: true}
		}
		if !d.ScriptApprovedAt.IsZero() {
			scriptApprovedAt = sql.NullInt64{Int64: timeutil.ToMillis(d.ScriptApprovedAt), Valid: true}
		}
		_, err := q.ExecContext(ctx, `INSERT INTO deliverables
			(id, story_id, position, kind, role, source_id, language, video, article, status, revision, pending,
			 failure, failed_step, session, approved_by, approved_at, published, publish_job, duration_ms, words, narrator,
			 script_approved_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET
			  status = excluded.status, revision = excluded.revision, pending = excluded.pending,
			  failure = excluded.failure, failed_step = excluded.failed_step, session = excluded.session,
			  approved_by = excluded.approved_by, approved_at = excluded.approved_at,
			  published = excluded.published, publish_job = excluded.publish_job, duration_ms = excluded.duration_ms, words = excluded.words,
			  narrator = excluded.narrator, script_approved_at = excluded.script_approved_at, updated_at = excluded.updated_at`,
			d.ID.String(), snap.ID.String(), i, d.Kind, d.Role, nullID(d.SourceID), d.Language, video, article,
			d.Status, d.Revision, pending, d.Failure, d.FailedStep, d.Session, nullID(d.ApprovedBy), approvedAt,
			published, nullID(d.PublishJob), nullMillis(d.Measures.Duration), nullPositive(d.Measures.Words), nullNarrator(d.Measures.Narrator),
			scriptApprovedAt, timeutil.ToMillis(d.CreatedAt), timeutil.ToMillis(d.UpdatedAt))
		if err != nil {
			return fmt.Errorf("write deliverable: %w", err)
		}
	}
	return nil
}

func (r *SQLite) Get(ctx context.Context, workspaceID, id uuid.UUID) (*storydomain.Story, error) {
	return r.getOne(ctx, `id = ? AND workspace_id = ?`, id.String(), workspaceID.String())
}

func (r *SQLite) GetByID(ctx context.Context, id uuid.UUID) (*storydomain.Story, error) {
	return r.getOne(ctx, `id = ?`, id.String())
}

func (r *SQLite) getOne(ctx context.Context, where string, args ...any) (*storydomain.Story, error) {
	stories, err := r.query(ctx, `SELECT `+storyColumns+` FROM stories WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	if len(stories) == 0 {
		return nil, notFound()
	}
	return stories[0], nil
}

func (r *SQLite) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*storydomain.Story, string, error) {
	where := []string{"workspace_id = ?"}
	args := []any{workspaceID.String()}
	if len(f.Statuses) > 0 {
		where = append(where, "status IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",")+")")
		for _, s := range f.Statuses {
			args = append(args, string(s))
		}
	}
	if f.PipelineID != uuid.Nil {
		where = append(where, "pipeline_id = ?")
		args = append(args, f.PipelineID.String())
	}
	if f.Origin != "" {
		where = append(where, "origin = ?")
		args = append(args, string(f.Origin))
	}
	if q := storydomain.Fingerprint(f.Query); q != "" {
		where = append(where, `fingerprint LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscaper.Replace(q)+"%")
	}
	var after cursor
	if ok, err := page.Decode(f.After, &after); err != nil {
		return nil, "", apperror.Wrap(apperror.ValidationFailed, err, "invalid cursor")
	} else if ok {
		where = append(where, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, after.At, after.At, after.ID)
	}
	limit := page.Limit(f.Limit, DefaultListLimit, MaxListLimit)
	args = append(args, limit+1)
	stories, err := r.query(ctx, `SELECT `+storyColumns+` FROM stories WHERE `+strings.Join(where, " AND ")+
		` ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
	if err != nil || len(stories) <= limit {
		return stories, "", err
	}
	stories = stories[:limit]
	return stories, nextCursor(stories[limit-1]), nil
}

func nextCursor(last *storydomain.Story) string {
	return page.Encode(cursor{At: timeutil.ToMillis(last.CreatedAt()), ID: last.ID().String()})
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// query loads stories and their deliverables from one snapshot, so a
// story is never paired with deliverables from a later save.
func (r *SQLite) query(ctx context.Context, query string, args ...any) (out []*storydomain.Story, err error) {
	err = r.q.Snapshot(ctx, func(ctx context.Context) error {
		out, err = r.load(ctx, query, args...)
		return err
	})
	return out, err
}

func (r *SQLite) load(ctx context.Context, query string, args ...any) ([]*storydomain.Story, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query stories: %w", err)
	}
	var snaps []storydomain.Snapshot
	for rows.Next() {
		var (
			s                                   storydomain.Snapshot
			rawID, rawWS, keywords, brief, stat string
			fingerprint                         string
			pipelineID, traceID, createdBy      sql.NullString
			created, updated                    int64
		)
		if err := rows.Scan(&rawID, &rawWS, &pipelineID, &traceID, &s.Origin, &s.Language, &s.Idea.Title, &s.Idea.Angle,
			&keywords, &fingerprint, &brief, &stat, &createdBy, &created, &updated, &s.Version); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan story: %w", err)
		}
		s.ID, _ = uuid.Parse(rawID)
		s.WorkspaceID, _ = uuid.Parse(rawWS)
		s.PipelineID = parseNullID(pipelineID)
		s.TraceID = parseNullID(traceID)
		s.CreatedBy = parseNullID(createdBy)
		_ = json.Unmarshal([]byte(keywords), &s.Idea.Keywords)
		_ = json.Unmarshal([]byte(brief), &s.Brief)
		s.CreatedAt, s.UpdatedAt = timeutil.FromMillis(created), timeutil.FromMillis(updated)
		snaps = append(snaps, s)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(snaps))
	for _, s := range snaps {
		ids = append(ids, s.ID)
	}
	byStory, err := r.deliverables(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*storydomain.Story, 0, len(snaps))
	for _, s := range snaps {
		s.Deliverables = byStory[s.ID]
		st, err := storydomain.Rehydrate(s)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// deliverables loads the deliverables of every story in ids in one
// query, by story, in position order.
func (r *SQLite) deliverables(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]storydomain.DeliverableSnapshot, error) {
	out := map[uuid.UUID][]storydomain.DeliverableSnapshot{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id.String())
	}
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT story_id, id, kind, role, source_id, language, video, article, status,
		revision, pending, failure, failed_step, session, approved_by, approved_at, published, publish_job, duration_ms, words,
		narrator, script_approved_at, created_at, updated_at
		FROM deliverables WHERE story_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)
		ORDER BY story_id, position`, args...)
	if err != nil {
		return nil, fmt.Errorf("query deliverables: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			d                                  storydomain.DeliverableSnapshot
			rawStory, rawID                    string
			sourceID, approvedBy               sql.NullString
			video, article, pending, published sql.NullString
			narrator, publishJob               sql.NullString
			approvedAt, durationMS, words      sql.NullInt64
			scriptApprovedAt                   sql.NullInt64
			created, updated                   int64
		)
		if err := rows.Scan(&rawStory, &rawID, &d.Kind, &d.Role, &sourceID, &d.Language, &video, &article, &d.Status,
			&d.Revision, &pending, &d.Failure, &d.FailedStep, &d.Session, &approvedBy, &approvedAt, &published, &publishJob,
			&durationMS, &words, &narrator, &scriptApprovedAt, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan deliverable: %w", err)
		}
		d.ID, _ = uuid.Parse(rawID)
		d.SourceID, d.ApprovedBy, d.PublishJob = parseNullID(sourceID), parseNullID(approvedBy), parseNullID(publishJob)
		if video.Valid {
			d.Video = &pipelinedomain.VideoSnapshot{}
			_ = json.Unmarshal([]byte(video.String), d.Video)
		}
		if article.Valid {
			d.Article = &pipelinedomain.ArticleSnapshot{}
			_ = json.Unmarshal([]byte(article.String), d.Article)
		}
		if pending.Valid {
			d.Pending = &storydomain.Feedback{}
			_ = json.Unmarshal([]byte(pending.String), d.Pending)
		}
		if published.Valid {
			d.Published = &storydomain.Publication{}
			_ = json.Unmarshal([]byte(published.String), d.Published)
		}
		if approvedAt.Valid {
			d.ApprovedAt = timeutil.FromMillis(approvedAt.Int64)
		}
		if scriptApprovedAt.Valid {
			d.ScriptApprovedAt = timeutil.FromMillis(scriptApprovedAt.Int64)
		}
		d.Measures = measures(durationMS, words, narrator)
		d.CreatedAt, d.UpdatedAt = timeutil.FromMillis(created), timeutil.FromMillis(updated)
		storyID, _ := uuid.Parse(rawStory)
		out[storyID] = append(out[storyID], d)
	}
	return out, rows.Err()
}

func (r *SQLite) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	q := r.q.W(ctx)
	res, err := q.ExecContext(ctx, `DELETE FROM stories WHERE id = ? AND workspace_id = ?`, id.String(), workspaceID.String())
	if err != nil {
		return fmt.Errorf("delete story: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound()
	}
	_, err = q.ExecContext(ctx, `DELETE FROM story_search WHERE story_id = ?`, id.String())
	return err
}

func (r *SQLite) PutRevision(ctx context.Context, rev storydomain.Revision) error {
	artifacts, _ := json.Marshal(nonNilMap(rev.Artifacts))
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO revisions
		(deliverable_id, number, feedback, artifacts, notes, edited_by, duration_ms, words, narrator, stage, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (deliverable_id, number) DO UPDATE SET artifacts = excluded.artifacts, notes = excluded.notes,
		  duration_ms = excluded.duration_ms, words = excluded.words, narrator = excluded.narrator`,
		rev.DeliverableID.String(), rev.Number, jsonOrNull(rev.Feedback), string(artifacts), rev.Notes, nullID(rev.EditedBy),
		nullMillis(rev.Measures.Duration), nullPositive(rev.Measures.Words), nullNarrator(rev.Measures.Narrator),
		string(rev.Stage), timeutil.ToMillis(rev.CreatedAt))
	if err != nil {
		return fmt.Errorf("write revision: %w", err)
	}
	return nil
}

func (r *SQLite) Revisions(ctx context.Context, deliverableID uuid.UUID) ([]storydomain.Revision, error) {
	return r.revisions(ctx, `deliverable_id = ? ORDER BY number`, deliverableID.String())
}

func (r *SQLite) Revision(ctx context.Context, deliverableID uuid.UUID, number int) (storydomain.Revision, error) {
	revs, err := r.revisions(ctx, `deliverable_id = ? AND number = ?`, deliverableID.String(), number)
	if err != nil {
		return storydomain.Revision{}, err
	}
	if len(revs) == 0 {
		return storydomain.Revision{}, apperror.New(apperror.NotFound, "revision not found")
	}
	return revs[0], nil
}

func (r *SQLite) revisions(ctx context.Context, where string, args ...any) ([]storydomain.Revision, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT deliverable_id, number, feedback, artifacts, notes, edited_by, duration_ms, words, narrator,
		stage, created_at
		FROM revisions WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("query revisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []storydomain.Revision
	for rows.Next() {
		var (
			rev       storydomain.Revision
			rawID     string
			feedback  sql.NullString
			artifacts string
			editedBy  sql.NullString
			duration  sql.NullInt64
			words     sql.NullInt64
			narrator  sql.NullString
			stage     string
			created   int64
		)
		if err := rows.Scan(&rawID, &rev.Number, &feedback, &artifacts, &rev.Notes, &editedBy, &duration, &words, &narrator,
			&stage, &created); err != nil {
			return nil, fmt.Errorf("scan revision: %w", err)
		}
		rev.DeliverableID, _ = uuid.Parse(rawID)
		if editedBy.Valid {
			rev.EditedBy, _ = uuid.Parse(editedBy.String)
		}
		if feedback.Valid {
			rev.Feedback = &storydomain.Feedback{}
			_ = json.Unmarshal([]byte(feedback.String), rev.Feedback)
		}
		_ = json.Unmarshal([]byte(artifacts), &rev.Artifacts)
		rev.Measures = measures(duration, words, narrator)
		rev.Stage = storydomain.Stage(stage)
		rev.CreatedAt = timeutil.FromMillis(created)
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (r *SQLite) Similar(ctx context.Context, workspaceID uuid.UUID, text string, limit int) ([]Match, error) {
	var terms []string
	for w := range strings.FieldsSeq(storydomain.Fingerprint(text)) {
		if len([]rune(w)) >= 3 {
			terms = append(terms, `"`+strings.ReplaceAll(w, `"`, ``)+`"`)
		}
	}
	if len(terms) == 0 {
		return nil, nil
	}
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT s.id, s.title, s.angle FROM story_search f
		JOIN stories s ON s.id = f.story_id
		WHERE story_search MATCH ? AND f.workspace_id = ? ORDER BY rank LIMIT 200`,
		strings.Join(terms, " OR "), workspaceID.String())
	if err != nil {
		return nil, fmt.Errorf("search stories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Match
	for rows.Next() {
		var (
			m     Match
			rawID string
		)
		if err := rows.Scan(&rawID, &m.Title, &m.Angle); err != nil {
			return nil, err
		}
		m.StoryID, _ = uuid.Parse(rawID)
		m.Score = storydomain.Similarity(text, m.Title)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rank(out, limit), nil
}

func rank(ms []Match, limit int) []Match {
	slices.SortStableFunc(ms, func(a, b Match) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return 0
	})
	if limit > 0 && len(ms) > limit {
		ms = ms[:limit]
	}
	return ms
}

func notFound() error { return apperror.New(apperror.StoryNotFound, "story not found") }

func nullID(id uuid.UUID) sql.NullString {
	if id == uuid.Nil {
		return sql.NullString{}
	}
	return sql.NullString{String: id.String(), Valid: true}
}

func parseNullID(s sql.NullString) uuid.UUID {
	if !s.Valid {
		return uuid.Nil
	}
	id, _ := uuid.Parse(s.String)
	return id
}

func jsonOrNull[T any](v *T) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// nullMillis stores a measured length; 0 (not measured) is NULL.
func nullMillis(d time.Duration) sql.NullInt64 {
	return sql.NullInt64{Int64: d.Milliseconds(), Valid: d > 0}
}

// nullPositive stores a count; 0 (not measured) is NULL.
func nullPositive(n int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(n), Valid: n > 0}
}

// nullNarrator stores the voice that read a narration; none is NULL.
func nullNarrator(n storydomain.Narrator) sql.NullString {
	if n.Engine == "" {
		return sql.NullString{}
	}
	return jsonOrNull(&n)
}

func measures(durationMS, words sql.NullInt64, narrator sql.NullString) storydomain.Measures {
	m := storydomain.Measures{
		Duration: time.Duration(durationMS.Int64) * time.Millisecond,
		Words:    int(words.Int64),
	}
	if narrator.Valid {
		_ = json.Unmarshal([]byte(narrator.String), &m.Narrator)
	}
	return m
}
