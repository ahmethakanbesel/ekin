-- +goose Up
CREATE TABLE document_headings (
    document_id TEXT    NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    level       INTEGER NOT NULL,
    title       TEXT    NOT NULL,
    page        INTEGER NOT NULL,
    PRIMARY KEY (document_id, position)
) STRICT, WITHOUT ROWID;

-- +goose Down
DROP TABLE document_headings;
