-- +goose Up
-- Self-review rounds join the rest of a pipeline's production settings,
-- where an absent value takes the workspace default. One round was the
-- default every pipeline got, so it becomes absent; 0 (off) is stored
-- as -1, since 0 now means unset.
UPDATE pipelines
SET production = json_set(COALESCE(production, '{}'), '$.selfReview',
    CASE self_review WHEN 0 THEN -1 ELSE self_review END)
WHERE self_review <> 1;
ALTER TABLE pipelines DROP COLUMN self_review;

-- +goose Down
ALTER TABLE pipelines ADD COLUMN self_review INTEGER NOT NULL DEFAULT 1;
UPDATE pipelines
SET self_review = CASE json_extract(production, '$.selfReview') WHEN -1 THEN 0 ELSE json_extract(production, '$.selfReview') END
WHERE json_extract(production, '$.selfReview') IS NOT NULL;
UPDATE pipelines
SET production = NULLIF(json_remove(production, '$.selfReview'), '{}')
WHERE production IS NOT NULL;
