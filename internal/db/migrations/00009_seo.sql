-- +goose Up
CREATE TABLE opportunities (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    keyword      TEXT    NOT NULL,
    norm         TEXT    NOT NULL,
    language     TEXT    NOT NULL,
    volume       INTEGER NOT NULL,
    difficulty   INTEGER NOT NULL,
    intent       TEXT    NOT NULL,
    note         TEXT    NOT NULL,
    source       TEXT    NOT NULL,
    status       TEXT    NOT NULL CHECK (status IN ('open', 'used', 'dismissed')),
    story_id     TEXT,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;

-- norm folds case, Turkish dotted/dotless i and accents (story.Fingerprint).
CREATE UNIQUE INDEX opportunities_keyword ON opportunities (workspace_id, norm, language);
CREATE INDEX opportunities_status ON opportunities (workspace_id, status);

-- +goose Down
DROP TABLE opportunities;
