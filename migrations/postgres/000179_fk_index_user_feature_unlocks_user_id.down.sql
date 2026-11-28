-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_feature_unlocks_user_id_fk;
