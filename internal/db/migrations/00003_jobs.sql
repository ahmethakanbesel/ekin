-- +goose Up
CREATE TABLE jobs (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    queue        TEXT    NOT NULL,
    kind         TEXT    NOT NULL,
    payload      TEXT    NOT NULL CHECK (json_valid(payload)),
    unique_key   TEXT,
    status       TEXT    NOT NULL,
    attempt      INTEGER NOT NULL,
    max_attempts INTEGER NOT NULL,
    run_after    INTEGER NOT NULL,
    lease_owner  TEXT,
    lease_until  INTEGER,
    last_error   TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    finished_at  INTEGER
) STRICT;

CREATE UNIQUE INDEX jobs_unique_key ON jobs (unique_key) WHERE unique_key IS NOT NULL;
CREATE INDEX jobs_claim ON jobs (queue, status, run_after);
CREATE INDEX jobs_workspace ON jobs (workspace_id, created_at);

-- +goose Down
DROP TABLE jobs;
