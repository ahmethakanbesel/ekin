-- +goose Up
CREATE TABLE pipelines (
    id             TEXT    PRIMARY KEY,
    workspace_id   TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name           TEXT    NOT NULL,
    language       TEXT    NOT NULL,
    -- {"video": {...} | null, "article": {...} | null, "lead": "video"|"article"}
    outputs        TEXT    NOT NULL CHECK (json_valid(outputs)),
    -- {"kind": "prompt"|"knowledge", "instructions": "...", "refs": [...]}
    source         TEXT    NOT NULL CHECK (json_valid(source)),
    schedule_cron  TEXT,
    schedule_tz    TEXT,
    schedule_batch INTEGER,
    status         TEXT    NOT NULL,
    next_run_at    INTEGER,
    last_run_at    INTEGER,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    version        INTEGER NOT NULL DEFAULT 1,
    CHECK ((schedule_cron IS NULL) = (schedule_tz IS NULL)
       AND (schedule_cron IS NULL) = (schedule_batch IS NULL))
) STRICT;

CREATE INDEX pipelines_workspace ON pipelines (workspace_id, created_at);
CREATE INDEX pipelines_due ON pipelines (next_run_at) WHERE next_run_at IS NOT NULL;

-- +goose Down
DROP TABLE pipelines;
