-- +goose Up
-- A person's note on a revision: its text, who wrote it and when.
ALTER TABLE revisions ADD COLUMN annotation TEXT NOT NULL DEFAULT '';
ALTER TABLE revisions ADD COLUMN annotated_by TEXT;
ALTER TABLE revisions ADD COLUMN annotated_at INTEGER;

-- +goose Down
ALTER TABLE revisions DROP COLUMN annotated_at;
ALTER TABLE revisions DROP COLUMN annotated_by;
ALTER TABLE revisions DROP COLUMN annotation;
