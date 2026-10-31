-- +goose Up
-- A workspace's AI visibility settings: brand, competitors, models, cap.
CREATE TABLE visibility_profiles (
    workspace_id TEXT    PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    settings     TEXT    NOT NULL CHECK (json_valid(settings)),
    next_run_at  INTEGER,
    updated_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX visibility_profiles_due ON visibility_profiles (next_run_at) WHERE next_run_at IS NOT NULL;

CREATE TABLE visibility_prompts (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    text         TEXT    NOT NULL,
    source       TEXT    NOT NULL CHECK (source IN ('person', 'agent')),
    active       INTEGER NOT NULL,
    covered_by   TEXT,
    covered_at   INTEGER,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX visibility_prompts_workspace ON visibility_prompts (workspace_id, created_at);

CREATE TABLE visibility_runs (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    status       TEXT    NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed', 'capped')),
    trigger      TEXT    NOT NULL,
    planned      INTEGER NOT NULL,
    done         INTEGER NOT NULL,
    failed       INTEGER NOT NULL,
    cost         REAL    NOT NULL,
    failure      TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    started_at   INTEGER,
    finished_at  INTEGER
) STRICT;

CREATE INDEX visibility_runs_workspace ON visibility_runs (workspace_id, created_at);

-- One model's answer to one prompt in a run. text is emptied after 90 days;
-- what was read from it (analysis, citations) is kept.
CREATE TABLE visibility_answers (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    run_id       TEXT    NOT NULL REFERENCES visibility_runs (id) ON DELETE CASCADE,
    prompt_id    TEXT    NOT NULL REFERENCES visibility_prompts (id) ON DELETE CASCADE,
    model        TEXT    NOT NULL,
    text         TEXT    NOT NULL,
    citations    TEXT    NOT NULL CHECK (json_valid(citations)),
    analysis     TEXT    NOT NULL CHECK (json_valid(analysis)),
    error        TEXT    NOT NULL,
    created_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX visibility_answers_run ON visibility_answers (run_id);
CREATE INDEX visibility_answers_prompt ON visibility_answers (prompt_id, created_at);
CREATE INDEX visibility_answers_month ON visibility_answers (workspace_id, created_at);

-- +goose Down
DROP TABLE visibility_answers;
DROP TABLE visibility_runs;
DROP TABLE visibility_prompts;
DROP TABLE visibility_profiles;
