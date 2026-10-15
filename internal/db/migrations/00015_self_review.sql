-- +goose Up
-- Rounds of agent self-review per draft; existing pipelines get the
-- default of one.
ALTER TABLE pipelines ADD COLUMN self_review INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE pipelines DROP COLUMN self_review;
