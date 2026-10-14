-- +goose Up
CREATE TABLE youtube_channels (
    workspace_id TEXT    PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    channel_id   TEXT    NOT NULL,
    title        TEXT    NOT NULL,
    sealed_token TEXT    NOT NULL,
    privacy      TEXT    NOT NULL CHECK (privacy IN ('private', 'unlisted', 'public')),
    connected_by TEXT,
    connected_at INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;

CREATE TABLE blog_targets (
    workspace_id TEXT    PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    config       TEXT    NOT NULL CHECK (json_valid(config)),
    updated_at   INTEGER NOT NULL
) STRICT;

-- +goose Down
DROP TABLE blog_targets;
DROP TABLE youtube_channels;
