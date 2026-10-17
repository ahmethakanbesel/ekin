-- +goose Up
-- Jobs sharing a concurrency key run one at a time (one run of a
-- pipeline at a time): a claim skips a job whose key another running job
-- holds with a live lease.
ALTER TABLE jobs ADD COLUMN concurrency_key TEXT;
CREATE INDEX jobs_concurrency ON jobs (concurrency_key, status) WHERE concurrency_key IS NOT NULL;

-- +goose Down
DROP INDEX jobs_concurrency;
ALTER TABLE jobs DROP COLUMN concurrency_key;
