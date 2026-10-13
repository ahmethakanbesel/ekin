-- +goose Up
CREATE TABLE stories (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    pipeline_id  TEXT    REFERENCES pipelines (id) ON DELETE SET NULL,
    origin       TEXT    NOT NULL,
    language     TEXT    NOT NULL,
    title        TEXT    NOT NULL,
    angle        TEXT    NOT NULL,
    keywords     TEXT    NOT NULL CHECK (json_valid(keywords)),
    fingerprint  TEXT    NOT NULL,
    brief        TEXT    NOT NULL CHECK (json_valid(brief)),
    status       TEXT    NOT NULL,
    created_by   TEXT,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    version      INTEGER NOT NULL
) STRICT;

CREATE INDEX stories_workspace ON stories (workspace_id, created_at);
CREATE INDEX stories_status ON stories (workspace_id, status, updated_at);
CREATE INDEX stories_pipeline ON stories (pipeline_id, created_at);

CREATE TABLE deliverables (
    id          TEXT    PRIMARY KEY,
    story_id    TEXT    NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    kind        TEXT    NOT NULL,
    role        TEXT    NOT NULL,
    source_id   TEXT,
    language    TEXT    NOT NULL,
    video       TEXT,
    article     TEXT,
    status      TEXT    NOT NULL,
    revision    INTEGER NOT NULL,
    pending     TEXT,
    failure     TEXT    NOT NULL DEFAULT '',
    failed_step TEXT    NOT NULL DEFAULT '',
    session     TEXT    NOT NULL DEFAULT '',
    approved_by TEXT,
    approved_at INTEGER,
    published   TEXT,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;

CREATE INDEX deliverables_story ON deliverables (story_id, position);

CREATE TABLE revisions (
    deliverable_id TEXT    NOT NULL REFERENCES deliverables (id) ON DELETE CASCADE,
    number         INTEGER NOT NULL,
    feedback       TEXT,
    artifacts      TEXT    NOT NULL CHECK (json_valid(artifacts)),
    notes          TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    PRIMARY KEY (deliverable_id, number)
) STRICT, WITHOUT ROWID;

-- Trigram index over title and angle for duplicate-idea candidates.
CREATE VIRTUAL TABLE story_search USING fts5 (
    story_id UNINDEXED,
    workspace_id UNINDEXED,
    text,
    tokenize = 'trigram'
);

-- +goose Down
DROP TABLE story_search;
DROP TABLE revisions;
DROP TABLE deliverables;
DROP TABLE stories;
