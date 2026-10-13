-- +goose Up
CREATE TABLE documents (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    title        TEXT    NOT NULL,
    filename     TEXT    NOT NULL,
    format       TEXT    NOT NULL CHECK (format IN ('pdf', 'txt', 'md', 'epub')),
    size         INTEGER NOT NULL,
    sha256       TEXT    NOT NULL,
    status       TEXT    NOT NULL CHECK (status IN ('pending', 'ready', 'failed')),
    page_count   INTEGER NOT NULL DEFAULT 0,
    tokens       INTEGER NOT NULL DEFAULT 0,
    failure      TEXT    NOT NULL DEFAULT '',
    created_by   TEXT,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX documents_workspace ON documents (workspace_id, created_at);

CREATE TABLE document_pages (
    document_id TEXT    NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    number      INTEGER NOT NULL,
    text        TEXT    NOT NULL,
    words       INTEGER NOT NULL,
    tokens      INTEGER NOT NULL,
    PRIMARY KEY (document_id, number)
) STRICT, WITHOUT ROWID;

-- +goose Down
DROP TABLE document_pages;
DROP TABLE documents;
