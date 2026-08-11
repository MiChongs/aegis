DROP TABLE IF EXISTS geo_stats_hourly;
DROP TABLE IF EXISTS user_geo_profiles;
DROP TABLE IF EXISTS geo_fences;
DROP FUNCTION IF EXISTS ensure_login_geo_partition(DATE);
DROP TABLE IF EXISTS login_geo_events;
DROP INDEX IF EXISTS idx_fw_logs_geom;
ALTER TABLE firewall_logs DROP COLUMN IF EXISTS geom;
