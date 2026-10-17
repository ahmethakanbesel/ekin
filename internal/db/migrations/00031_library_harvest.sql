-- +goose Up
-- Library assets saved from drafts: provisional until their deliverable
-- is decided, kept once a person saved or edited them, and where they
-- came from. Assets so far are uploads: kept, not provisional.
ALTER TABLE library_assets ADD COLUMN provisional INTEGER NOT NULL DEFAULT 0;
ALTER TABLE library_assets ADD COLUMN kept INTEGER NOT NULL DEFAULT 1;
ALTER TABLE library_assets ADD COLUMN harvest_story TEXT;
ALTER TABLE library_assets ADD COLUMN harvest_deliverable TEXT;
ALTER TABLE library_assets ADD COLUMN harvest_revision INTEGER NOT NULL DEFAULT 0;
-- The outside item ("pexels:video:123"), so it is saved once.
ALTER TABLE library_assets ADD COLUMN source_ref TEXT;
ALTER TABLE library_assets ADD COLUMN source_url TEXT NOT NULL DEFAULT '';
ALTER TABLE library_assets ADD COLUMN original_url TEXT NOT NULL DEFAULT '';
ALTER TABLE library_assets ADD COLUMN derived_from TEXT;

CREATE UNIQUE INDEX library_assets_source ON library_assets (workspace_id, source_ref) WHERE source_ref IS NOT NULL;
CREATE INDEX library_assets_harvest ON library_assets (harvest_deliverable) WHERE harvest_deliverable IS NOT NULL;

-- Which kinds a workspace saves from its drafts; no row saves nothing.
CREATE TABLE library_settings (
    workspace_id TEXT    PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    music        INTEGER NOT NULL DEFAULT 0,
    video        INTEGER NOT NULL DEFAULT 0,
    photo        INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL
) STRICT;

-- +goose Down
DROP TABLE library_settings;
DROP INDEX library_assets_harvest;
DROP INDEX library_assets_source;
ALTER TABLE library_assets DROP COLUMN derived_from;
ALTER TABLE library_assets DROP COLUMN original_url;
ALTER TABLE library_assets DROP COLUMN source_url;
ALTER TABLE library_assets DROP COLUMN source_ref;
ALTER TABLE library_assets DROP COLUMN harvest_revision;
ALTER TABLE library_assets DROP COLUMN harvest_deliverable;
ALTER TABLE library_assets DROP COLUMN harvest_story;
ALTER TABLE library_assets DROP COLUMN kept;
ALTER TABLE library_assets DROP COLUMN provisional;
