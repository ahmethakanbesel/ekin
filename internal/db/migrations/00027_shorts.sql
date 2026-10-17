-- +goose Up
-- Shorts cut from long videos (docs/shorts.md): the span of its long
-- video a short speaks, and the moments found in long videos.
ALTER TABLE deliverables ADD COLUMN clip_start_ms INTEGER;
ALTER TABLE deliverables ADD COLUMN clip_end_ms INTEGER;

CREATE TABLE story_moments (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    story_id     TEXT    NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    source_id    TEXT    NOT NULL REFERENCES deliverables (id) ON DELETE CASCADE,
    start_ms     INTEGER NOT NULL,
    end_ms       INTEGER NOT NULL CHECK (end_ms > start_ms),
    hook         TEXT    NOT NULL DEFAULT '',
    reason       TEXT    NOT NULL DEFAULT '',
    score        INTEGER NOT NULL DEFAULT 0,
    origin       TEXT    NOT NULL,
    status       TEXT    NOT NULL,
    short_id     TEXT,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX story_moments_story ON story_moments (story_id, start_ms);

-- +goose Down
DROP TABLE story_moments;
ALTER TABLE deliverables DROP COLUMN clip_end_ms;
ALTER TABLE deliverables DROP COLUMN clip_start_ms;
