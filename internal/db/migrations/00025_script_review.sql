-- +goose Up
-- When a person approved a video's script, for stories that review the
-- script before the video is built; and which revisions hold only that
-- script.
ALTER TABLE deliverables ADD COLUMN script_approved_at INTEGER;
ALTER TABLE revisions ADD COLUMN stage TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE revisions DROP COLUMN stage;
ALTER TABLE deliverables DROP COLUMN script_approved_at;
