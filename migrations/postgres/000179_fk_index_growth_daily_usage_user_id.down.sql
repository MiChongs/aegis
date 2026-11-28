-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_growth_daily_usage_user_id_fk;
