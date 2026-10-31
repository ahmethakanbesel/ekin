-- +goose Up
-- The command that builds the site from its repository, run on every change.
ALTER TABLE sites ADD COLUMN build TEXT NOT NULL DEFAULT '';
-- The story whose change sets out to fix a finding, and since when.
ALTER TABLE site_findings ADD COLUMN claimed_by TEXT;
ALTER TABLE site_findings ADD COLUMN claimed_at INTEGER;

-- +goose Down
ALTER TABLE site_findings DROP COLUMN claimed_at;
ALTER TABLE site_findings DROP COLUMN claimed_by;
ALTER TABLE sites DROP COLUMN build;
