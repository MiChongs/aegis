-- +migrate Down
DROP INDEX IF EXISTS idx_sign_daily_rollups_date;
DROP INDEX IF EXISTS idx_sign_stats_app_total_sign_days;
DROP INDEX IF EXISTS idx_sign_stats_app_consecutive;
DROP TABLE IF EXISTS sign_daily_rollups;
DROP TABLE IF EXISTS sign_stats;
