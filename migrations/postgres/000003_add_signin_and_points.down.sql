-- +migrate Down
DROP INDEX IF EXISTS idx_daily_signins_app_sign_date;
DROP INDEX IF EXISTS idx_daily_signins_user_app_date;
DROP INDEX IF EXISTS idx_daily_signins_user_signed_at;
DROP TABLE IF EXISTS daily_signins;

ALTER TABLE users
    DROP COLUMN IF EXISTS experience,
    DROP COLUMN IF EXISTS integral;
