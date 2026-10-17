-- +goose Up
-- The job publishing a deliverable; only its retry may resume a
-- publication cut short.
ALTER TABLE deliverables ADD COLUMN publish_job TEXT;

-- +goose Down
ALTER TABLE deliverables DROP COLUMN publish_job;
