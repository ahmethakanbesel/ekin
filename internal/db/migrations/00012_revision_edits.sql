-- +goose Up
ALTER TABLE revisions ADD COLUMN edited_by TEXT;

-- +goose Down
ALTER TABLE revisions DROP COLUMN edited_by;
