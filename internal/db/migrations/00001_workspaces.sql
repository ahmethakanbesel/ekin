-- +goose Up
CREATE TABLE workspaces (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL,
    timezone   TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

-- +goose Down
DROP TABLE workspaces;
