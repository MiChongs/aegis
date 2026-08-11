-- +migrate Down

ALTER TABLE payment_orders DROP COLUMN IF EXISTS currency;
