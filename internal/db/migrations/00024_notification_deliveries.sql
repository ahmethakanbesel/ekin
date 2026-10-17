-- +goose Up
-- Who a notification was already emailed to while its digest is still
-- going out: a retry after a partial failure emails only the rest. Rows
-- go when the notification is marked emailed.
CREATE TABLE notification_deliveries (
    notification_id TEXT    NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    recipient       TEXT    NOT NULL,
    delivered_at    INTEGER NOT NULL,
    PRIMARY KEY (notification_id, recipient)
) STRICT, WITHOUT ROWID;

-- +goose Down
DROP TABLE notification_deliveries;
