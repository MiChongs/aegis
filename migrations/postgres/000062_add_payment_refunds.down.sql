-- +migrate Down

DROP TABLE IF EXISTS payment_refunds;

DROP INDEX IF EXISTS idx_payment_orders_refund_status;
ALTER TABLE payment_orders DROP CONSTRAINT IF EXISTS ck_payment_orders_refunded_amount;
ALTER TABLE payment_orders DROP COLUMN IF EXISTS refund_status;
ALTER TABLE payment_orders DROP COLUMN IF EXISTS refunded_amount;
