-- +migrate Down
DROP INDEX IF EXISTS idx_platform_banners_window;
DROP INDEX IF EXISTS idx_platform_banners_active;
DROP TABLE IF EXISTS platform_banners;
