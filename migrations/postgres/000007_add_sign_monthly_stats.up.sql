-- +migrate Up
CREATE TABLE IF NOT EXISTS sign_monthly_stats (
    month_key DATE NOT NULL,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sign_days BIGINT NOT NULL DEFAULT 0,
    total_integral_reward BIGINT NOT NULL DEFAULT 0,
    total_experience_reward BIGINT NOT NULL DEFAULT 0,
    last_sign_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (month_key, appid, user_id)
);

CREATE INDEX IF NOT EXISTS idx_sign_monthly_stats_app_rank ON sign_monthly_stats(month_key, appid, sign_days DESC, last_sign_at ASC, user_id ASC);

INSERT INTO sign_monthly_stats (
    month_key,
    appid,
    user_id,
    sign_days,
    total_integral_reward,
    total_experience_reward,
    last_sign_at,
    updated_at
)
SELECT
    date_trunc('month', sign_date)::date AS month_key,
    appid,
    user_id,
    COUNT(*) AS sign_days,
    COALESCE(SUM(integral_reward), 0) AS total_integral_reward,
    COALESCE(SUM(experience_reward), 0) AS total_experience_reward,
    MAX(signed_at) AS last_sign_at,
    NOW()
FROM daily_signins
GROUP BY date_trunc('month', sign_date)::date, appid, user_id
ON CONFLICT (month_key, appid, user_id) DO UPDATE SET
    sign_days = EXCLUDED.sign_days,
    total_integral_reward = EXCLUDED.total_integral_reward,
    total_experience_reward = EXCLUDED.total_experience_reward,
    last_sign_at = EXCLUDED.last_sign_at,
    updated_at = NOW();
