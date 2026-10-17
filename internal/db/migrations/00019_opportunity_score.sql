-- +goose Up
-- An opportunity's score (seodomain.Opportunity.Score), stored so the
-- pool is sorted, filtered and paged in SQL. Its inputs never change after
-- import, so it is written once; existing rows are scored here the same
-- way.
ALTER TABLE opportunities ADD COLUMN score REAL NOT NULL DEFAULT 0;
UPDATE opportunities SET score =
    (CASE WHEN volume = 0 THEN 10.0 ELSE volume END)
  * (0.2 + CASE WHEN difficulty >= 0 THEN 1 - difficulty / 100.0 ELSE 0.5 END)
  * (CASE WHEN intent = 'informational' THEN 1.3 ELSE 1 END);
CREATE INDEX opportunities_rank ON opportunities (workspace_id, status, score DESC, id);

-- +goose Down
DROP INDEX opportunities_rank;
ALTER TABLE opportunities DROP COLUMN score;
