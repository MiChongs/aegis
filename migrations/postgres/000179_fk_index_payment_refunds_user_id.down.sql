-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_payment_refunds_user_id_fk;
