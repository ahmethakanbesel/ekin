-- +goose Up
-- A trace groups the jobs of one request: the job that started it and
-- every job it caused. started_at is when the latest attempt began.
ALTER TABLE jobs ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN started_at INTEGER;
ALTER TABLE stories ADD COLUMN trace_id TEXT;

-- Best effort for earlier rows: a story joins the planning job that was
-- running when it was created (or else is a trace of its own), and a
-- step job joins its story's trace.
UPDATE stories SET trace_id = COALESCE((
    SELECT j.id FROM jobs j
    WHERE j.workspace_id = stories.workspace_id
      AND j.kind IN ('story.adhoc', 'pipeline.run')
      AND j.created_at <= stories.created_at
      AND (j.finished_at IS NULL OR j.finished_at >= stories.created_at)
    ORDER BY j.created_at DESC LIMIT 1), id);
UPDATE jobs SET trace_id = COALESCE(
    (SELECT s.trace_id FROM stories s WHERE s.id = json_extract(jobs.payload, '$.storyId')), id);
CREATE INDEX jobs_trace ON jobs (workspace_id, trace_id);

-- +goose Down
DROP INDEX jobs_trace;
ALTER TABLE stories DROP COLUMN trace_id;
ALTER TABLE jobs DROP COLUMN started_at;
ALTER TABLE jobs DROP COLUMN trace_id;
