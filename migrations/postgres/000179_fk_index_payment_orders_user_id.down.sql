-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_payment_orders_user_id_fk;
