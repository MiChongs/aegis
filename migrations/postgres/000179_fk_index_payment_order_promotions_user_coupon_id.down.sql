-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_payment_order_promotions_user_coupon_id_fk;
