-- +goose Up
-- The workspace's website: where it lives and how often it is crawled.
CREATE TABLE sites (
    workspace_id  TEXT    PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    url           TEXT    NOT NULL,
    max_pages     INTEGER NOT NULL,
    schedule      TEXT    NOT NULL CHECK (schedule IN ('off', 'daily', 'weekly')),
    next_crawl_at INTEGER,
    updated_by    TEXT,
    updated_at    INTEGER NOT NULL
) STRICT;

CREATE INDEX sites_due ON sites (next_crawl_at) WHERE next_crawl_at IS NOT NULL;

CREATE TABLE site_crawls (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    url          TEXT    NOT NULL,
    status       TEXT    NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed')),
    trigger      TEXT    NOT NULL,
    requested_by TEXT,
    pages        INTEGER NOT NULL,
    facts        TEXT    NOT NULL,
    failure      TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    started_at   INTEGER,
    finished_at  INTEGER
) STRICT;

CREATE INDEX site_crawls_workspace ON site_crawls (workspace_id, created_at);

-- The latest finished crawl's pages; data holds the page's details as JSON.
CREATE TABLE site_pages (
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    url          TEXT    NOT NULL,
    status       INTEGER NOT NULL,
    class        TEXT    NOT NULL CHECK (class IN ('ok', 'redirect', 'error')),
    search       TEXT    NOT NULL,
    data         TEXT    NOT NULL,
    first_seen   INTEGER NOT NULL,
    last_seen    INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, url)
) STRICT;

CREATE TABLE site_findings (
    id            TEXT    PRIMARY KEY,
    workspace_id  TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    rule          TEXT    NOT NULL,
    url           TEXT    NOT NULL,
    -- site.Severity.Rank of the rule, kept for ordering.
    severity_rank INTEGER NOT NULL,
    status        TEXT    NOT NULL CHECK (status IN ('open', 'resolved', 'ignored')),
    evidence      TEXT    NOT NULL,
    crawl_id      TEXT,
    first_seen    INTEGER NOT NULL,
    last_seen     INTEGER NOT NULL,
    resolved_at   INTEGER,
    updated_at    INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX site_findings_key ON site_findings (workspace_id, rule, url);
CREATE INDEX site_findings_list ON site_findings (workspace_id, status, severity_rank, rule, url);
CREATE INDEX site_findings_page ON site_findings (workspace_id, url, status);

-- +goose Down
DROP TABLE site_findings;
DROP TABLE site_pages;
DROP TABLE site_crawls;
DROP TABLE sites;
