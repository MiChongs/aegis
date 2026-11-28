-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_ad_free_passes_user_id_fk;
