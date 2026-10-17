-- +goose Up
-- A pipeline without a template now takes the workspace's default
-- template; no_template asks for none explicitly.
ALTER TABLE pipelines ADD COLUMN no_template INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE pipelines DROP COLUMN no_template;
