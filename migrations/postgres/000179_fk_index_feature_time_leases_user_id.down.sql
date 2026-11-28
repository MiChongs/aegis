-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_feature_time_leases_user_id_fk;
