-- +goose Up
-- The scale and frame rate a revision's preview was rendered at, so
-- approving reuses it as the final video only when it covers the final
-- render's settings.
ALTER TABLE revisions ADD COLUMN preview_scale REAL;
ALTER TABLE revisions ADD COLUMN preview_fps INTEGER;

-- +goose Down
ALTER TABLE revisions DROP COLUMN preview_fps;
ALTER TABLE revisions DROP COLUMN preview_scale;
