-- +migrate Up
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS integral BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS experience BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS daily_signins (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    signed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sign_date DATE NOT NULL,
    integral_reward BIGINT NOT NULL DEFAULT 0,
    experience_reward BIGINT NOT NULL DEFAULT 0,
    integral_before BIGINT NOT NULL DEFAULT 0,
    integral_after BIGINT NOT NULL DEFAULT 0,
    experience_before BIGINT NOT NULL DEFAULT 0,
    experience_after BIGINT NOT NULL DEFAULT 0,
    consecutive_days INTEGER NOT NULL DEFAULT 1,
    reward_multiplier NUMERIC(4,2) NOT NULL DEFAULT 1.00,
    bonus_type VARCHAR(64) NULL,
    bonus_description VARCHAR(255) NULL,
    sign_in_source VARCHAR(16) NOT NULL DEFAULT 'manual',
    device_info VARCHAR(128) NULL,
    ip_address VARCHAR(64) NULL,
    location VARCHAR(255) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_daily_signins_user_app_date UNIQUE (user_id, appid, sign_date)
);

CREATE INDEX IF NOT EXISTS idx_daily_signins_user_signed_at ON daily_signins(user_id, signed_at DESC);
CREATE INDEX IF NOT EXISTS idx_daily_signins_user_app_date ON daily_signins(user_id, appid, sign_date DESC);
CREATE INDEX IF NOT EXISTS idx_daily_signins_app_sign_date ON daily_signins(appid, sign_date DESC);
