-- +migrate Down
DROP INDEX IF EXISTS idx_avatar_assets_config;
DROP INDEX IF EXISTS idx_avatar_assets_owner;
DROP INDEX IF EXISTS uq_avatar_assets_active;
DROP TABLE IF EXISTS avatar_assets;
