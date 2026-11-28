-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_sign_monthly_stats_user_id_fk;
