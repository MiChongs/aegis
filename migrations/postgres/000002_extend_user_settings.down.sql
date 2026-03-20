-- +migrate Down
DROP INDEX IF EXISTS idx_user_settings_user_updated_at;

ALTER TABLE user_settings
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS is_active,
    DROP COLUMN IF EXISTS version;
