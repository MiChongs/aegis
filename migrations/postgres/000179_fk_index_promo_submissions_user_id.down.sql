-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_promo_submissions_user_id_fk;
