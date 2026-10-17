-- +goose Up
-- Which revisions used which library assets; reuse is unlimited, only
-- counted.
CREATE TABLE library_usages (
    asset_id       TEXT    NOT NULL REFERENCES library_assets (id) ON DELETE CASCADE,
    workspace_id   TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    story_id       TEXT    NOT NULL,
    deliverable_id TEXT    NOT NULL,
    revision       INTEGER NOT NULL,
    used_at        INTEGER NOT NULL,
    PRIMARY KEY (asset_id, deliverable_id, revision)
) STRICT, WITHOUT ROWID;

CREATE INDEX library_usages_deliverable ON library_usages (deliverable_id);

-- +goose Down
DROP TABLE library_usages;
