-- +goose Up
-- The media library: music, stock videos and photos kept for reuse.
CREATE TABLE library_assets (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind         TEXT    NOT NULL CHECK (kind IN ('music', 'video', 'photo')),
    origin       TEXT    NOT NULL CHECK (origin IN ('generated', 'pexels', 'upload')),
    status       TEXT    NOT NULL CHECK (status IN ('processing', 'ready', 'failed')),
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    -- JSON array of lowercase tags.
    tags         TEXT    NOT NULL DEFAULT '[]',
    mood         TEXT    NOT NULL DEFAULT '',
    tempo        INTEGER NOT NULL DEFAULT 0,
    -- JSON {author, url, licence}.
    credit       TEXT    NOT NULL DEFAULT '{}',
    filename     TEXT    NOT NULL DEFAULT '',
    size         INTEGER NOT NULL DEFAULT 0,
    -- Of the file received, so the same file is kept once per workspace.
    sha256       TEXT    NOT NULL,
    -- Extension of the main file once ingested.
    format       TEXT    NOT NULL DEFAULT '',
    width        INTEGER NOT NULL DEFAULT 0,
    height       INTEGER NOT NULL DEFAULT 0,
    -- Derived from width and height, for filtering.
    orientation  TEXT    NOT NULL DEFAULT '',
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    poster       INTEGER NOT NULL DEFAULT 0,
    failure      TEXT    NOT NULL DEFAULT '',
    created_by   TEXT,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    archived_at  INTEGER
) STRICT;

CREATE INDEX library_assets_workspace ON library_assets (workspace_id, created_at);
CREATE UNIQUE INDEX library_assets_sha256 ON library_assets (workspace_id, sha256);

-- +goose Down
DROP TABLE library_assets;
