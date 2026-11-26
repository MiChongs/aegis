-- +migrate Down
DROP INDEX IF EXISTS idx_email_deliveries_pending_poll;
