-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_feature_time_ledger_user_id_fk;
