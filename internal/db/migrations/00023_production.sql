-- +goose Up
-- How a pipeline's stories are made (drafting model and effort, and
-- later settings), as JSON; NULL takes every default.
ALTER TABLE pipelines ADD COLUMN production TEXT CHECK (production IS NULL OR json_valid(production));

-- +goose Down
ALTER TABLE pipelines DROP COLUMN production;
