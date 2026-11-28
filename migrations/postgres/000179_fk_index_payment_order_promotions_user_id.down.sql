-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_payment_order_promotions_user_id_fk;
