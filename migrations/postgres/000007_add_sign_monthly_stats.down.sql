-- +migrate Down
DROP INDEX IF EXISTS idx_sign_monthly_stats_app_rank;
DROP TABLE IF EXISTS sign_monthly_stats;
