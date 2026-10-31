-- +goose Up
-- Rival sites whose sitemaps are compared week by week.
CREATE TABLE site_competitors (
    id            TEXT    PRIMARY KEY,
    workspace_id  TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name          TEXT    NOT NULL,
    url           TEXT    NOT NULL,
    pages         INTEGER NOT NULL,
    last_check_at INTEGER,
    last_error    TEXT    NOT NULL,
    next_check_at INTEGER NOT NULL,
    created_at    INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX site_competitors_url ON site_competitors (workspace_id, url);
CREATE INDEX site_competitors_due ON site_competitors (next_check_at);

CREATE TABLE site_competitor_pages (
    competitor_id TEXT    NOT NULL REFERENCES site_competitors (id) ON DELETE CASCADE,
    url           TEXT    NOT NULL,
    title         TEXT    NOT NULL,
    first_seen    INTEGER NOT NULL,
    last_seen     INTEGER NOT NULL,
    removed_at    INTEGER,
    PRIMARY KEY (competitor_id, url)
) STRICT;

-- Pages a competitor added or dropped, newest first.
CREATE TABLE site_competitor_changes (
    id            TEXT    PRIMARY KEY,
    workspace_id  TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    competitor_id TEXT    NOT NULL REFERENCES site_competitors (id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL CHECK (kind IN ('added', 'removed')),
    url           TEXT    NOT NULL,
    title         TEXT    NOT NULL,
    at            INTEGER NOT NULL
) STRICT;

CREATE INDEX site_competitor_changes_feed ON site_competitor_changes (workspace_id, at DESC, id DESC);

-- +goose Down
DROP TABLE site_competitor_changes;
DROP TABLE site_competitor_pages;
DROP TABLE site_competitors;
