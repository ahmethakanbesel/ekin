-- +goose Up
-- Templates are edited with optimistic concurrency, like pipelines.
ALTER TABLE templates ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE templates DROP COLUMN version;
