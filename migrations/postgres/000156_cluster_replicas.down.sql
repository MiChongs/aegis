DROP SEQUENCE IF EXISTS transaction_no_node_seq;
ALTER TABLE storage_migrations DROP COLUMN IF EXISTS heartbeat_at;
ALTER TABLE storage_migrations DROP COLUMN IF EXISTS owner;
DROP INDEX IF EXISTS idx_ban_appeals_unreviewed_open;
ALTER TABLE ban_appeals DROP COLUMN IF EXISTS review_claimed_by;
ALTER TABLE ban_appeals DROP COLUMN IF EXISTS review_claimed_at;
DROP INDEX IF EXISTS idx_payment_receipt_exports_expires;
DROP TABLE IF EXISTS payment_receipt_exports;
