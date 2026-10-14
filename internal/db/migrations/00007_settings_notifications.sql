-- +goose Up
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN email_notifications INTEGER NOT NULL DEFAULT 1;

-- The first account is the instance admin.
UPDATE users SET is_admin = 1 WHERE id = (SELECT id FROM users ORDER BY created_at LIMIT 1);

-- Instance settings, one JSON document per key (e.g. "profile:vps").
CREATE TABLE settings (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL CHECK (json_valid(value)),
    updated_by TEXT,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE notifications (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    story_id     TEXT,
    kind         TEXT    NOT NULL,
    title        TEXT    NOT NULL,
    detail       TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    emailed_at   INTEGER
) STRICT;

CREATE INDEX notifications_feed ON notifications (workspace_id, created_at);
CREATE INDEX notifications_unsent ON notifications (emailed_at) WHERE emailed_at IS NULL;

-- +goose Down
DROP TABLE notifications;
DROP TABLE settings;
ALTER TABLE users DROP COLUMN email_notifications;
ALTER TABLE users DROP COLUMN is_admin;
