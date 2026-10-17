-- +goose Up
-- A long video's thumbnail variants: the one a person picked (0 for the
-- recommended one) and, per revision, the variants as JSON.
ALTER TABLE deliverables ADD COLUMN thumbnail INTEGER NOT NULL DEFAULT 0;
ALTER TABLE revisions ADD COLUMN thumbnails TEXT;

-- +goose Down
ALTER TABLE revisions DROP COLUMN thumbnails;
ALTER TABLE deliverables DROP COLUMN thumbnail;
