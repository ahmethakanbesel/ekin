-- +goose Up
-- What a revision came out as: the rendered video's length and the words
-- spoken or written. NULL until measured; the deliverable keeps its
-- latest revision's for listings.
ALTER TABLE revisions ADD COLUMN duration_ms INTEGER;
ALTER TABLE revisions ADD COLUMN words INTEGER;
ALTER TABLE deliverables ADD COLUMN duration_ms INTEGER;
ALTER TABLE deliverables ADD COLUMN words INTEGER;

-- +goose Down
ALTER TABLE deliverables DROP COLUMN words;
ALTER TABLE deliverables DROP COLUMN duration_ms;
ALTER TABLE revisions DROP COLUMN words;
ALTER TABLE revisions DROP COLUMN duration_ms;
