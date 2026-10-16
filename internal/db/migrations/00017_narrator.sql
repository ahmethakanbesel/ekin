-- +goose Up
-- The voice that read a video's narration, as JSON {engine, voice, model};
-- NULL when nothing was narrated or it is not known.
ALTER TABLE revisions ADD COLUMN narrator TEXT CHECK (narrator IS NULL OR json_valid(narrator));
ALTER TABLE deliverables ADD COLUMN narrator TEXT CHECK (narrator IS NULL OR json_valid(narrator));

-- +goose Down
ALTER TABLE deliverables DROP COLUMN narrator;
ALTER TABLE revisions DROP COLUMN narrator;
