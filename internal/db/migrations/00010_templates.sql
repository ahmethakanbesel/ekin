-- +goose Up
CREATE TABLE templates (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    spec         TEXT    NOT NULL CHECK (json_valid(spec)),
    source       TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX templates_workspace ON templates (workspace_id, created_at);

ALTER TABLE pipelines ADD COLUMN template_id TEXT;

-- +goose Down
ALTER TABLE pipelines DROP COLUMN template_id;
DROP TABLE templates;
