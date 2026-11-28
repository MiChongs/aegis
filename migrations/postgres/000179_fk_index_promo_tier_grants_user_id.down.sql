-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_promo_tier_grants_user_id_fk;
