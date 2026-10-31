-- +goose Up
-- A workspace's Search Console connection; the refresh token is sealed.
CREATE TABLE search_connections (
    workspace_id   TEXT    PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    sealed_token   TEXT    NOT NULL,
    property       TEXT    NOT NULL,
    connected_by   TEXT,
    connected_at   INTEGER NOT NULL,
    synced_through INTEGER,
    last_sync_at   INTEGER,
    last_error     TEXT    NOT NULL,
    updated_at     INTEGER NOT NULL
) STRICT;

CREATE INDEX search_connections_sync ON search_connections (last_sync_at) WHERE property != '';

-- Daily search figures per page or query (dimension), aggregates only.
CREATE TABLE search_metrics (
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    dimension    TEXT    NOT NULL CHECK (dimension IN ('page', 'query')),
    day          INTEGER NOT NULL,
    key          TEXT    NOT NULL,
    clicks       INTEGER NOT NULL,
    impressions  INTEGER NOT NULL,
    position     REAL    NOT NULL,
    PRIMARY KEY (workspace_id, dimension, day, key)
) STRICT;

CREATE INDEX search_metrics_key ON search_metrics (workspace_id, dimension, key, day);

-- +goose Down
DROP TABLE search_metrics;
DROP TABLE search_connections;
