-- +migrate Down
DROP INDEX IF EXISTS idx_payment_orders_review;
ALTER TABLE payment_orders
    DROP COLUMN IF EXISTS paid_amount,
    DROP COLUMN IF EXISTS review_reason,
    DROP COLUMN IF EXISTS review_note,
    DROP COLUMN IF EXISTS review_at;
