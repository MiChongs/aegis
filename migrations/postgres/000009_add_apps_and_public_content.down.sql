-- +migrate Down
DROP INDEX IF EXISTS idx_notices_app_created;
DROP INDEX IF EXISTS idx_banners_app_status_time;
DROP INDEX IF EXISTS idx_banners_app_position;
DROP INDEX IF EXISTS idx_apps_status;
DROP TABLE IF EXISTS notices;
DROP TABLE IF EXISTS banners;
DROP TABLE IF EXISTS apps;
