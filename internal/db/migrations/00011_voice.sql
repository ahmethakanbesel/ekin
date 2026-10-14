-- +goose Up
ALTER TABLE pipelines ADD COLUMN voice TEXT;

-- +goose Down
ALTER TABLE pipelines DROP COLUMN voice;
