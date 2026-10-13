-- +goose Up
CREATE TABLE users (
    id            TEXT    PRIMARY KEY,
    email         TEXT    NOT NULL UNIQUE,
    name          TEXT    NOT NULL,
    password_hash TEXT    NOT NULL,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
) STRICT;

-- id is the SHA-256 of the cookie token; the token itself is never stored.
CREATE TABLE sessions (
    id           TEXT    PRIMARY KEY,
    user_id      TEXT    NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX sessions_expiry ON sessions (expires_at);

CREATE TABLE memberships (
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      TEXT    NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         TEXT    NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
) STRICT;

CREATE INDEX memberships_user ON memberships (user_id);

CREATE TABLE invitations (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        TEXT    NOT NULL,
    role         TEXT    NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
    invited_by   TEXT    REFERENCES users (id) ON DELETE SET NULL,
    token_hash   TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
) STRICT;

-- One live invitation per address per workspace; re-inviting replaces it.
CREATE UNIQUE INDEX invitations_workspace_email ON invitations (workspace_id, email);
CREATE INDEX invitations_expiry ON invitations (expires_at);

-- +goose Down
DROP TABLE invitations;
DROP TABLE memberships;
DROP TABLE sessions;
DROP TABLE users;
