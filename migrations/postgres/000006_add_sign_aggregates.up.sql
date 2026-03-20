-- +migrate Up
CREATE TABLE IF NOT EXISTS sign_stats (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    last_sign_date DATE NULL,
    last_sign_at TIMESTAMPTZ NULL,
    consecutive_days INTEGER NOT NULL DEFAULT 0,
    total_sign_days BIGINT NOT NULL DEFAULT 0,
    total_integral_reward BIGINT NOT NULL DEFAULT 0,
    total_experience_reward BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, appid)
);

CREATE TABLE IF NOT EXISTS sign_daily_rollups (
    appid BIGINT NOT NULL,
    rollup_date DATE NOT NULL,
    sign_user_count BIGINT NOT NULL DEFAULT 0,
    total_integral_reward BIGINT NOT NULL DEFAULT 0,
    total_experience_reward BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, rollup_date)
);

CREATE INDEX IF NOT EXISTS idx_sign_stats_app_consecutive ON sign_stats(appid, consecutive_days DESC, total_sign_days DESC, user_id ASC);
CREATE INDEX IF NOT EXISTS idx_sign_stats_app_total_sign_days ON sign_stats(appid, total_sign_days DESC, user_id ASC);
CREATE INDEX IF NOT EXISTS idx_sign_daily_rollups_date ON sign_daily_rollups(rollup_date DESC, appid);

INSERT INTO sign_stats (
    user_id,
    appid,
    last_sign_date,
    last_sign_at,
    consecutive_days,
    total_sign_days,
    total_integral_reward,
    total_experience_reward,
    updated_at
)
SELECT
    summary.user_id,
    summary.appid,
    latest.sign_date,
    latest.signed_at,
    latest.consecutive_days,
    summary.total_sign_days,
    summary.total_integral_reward,
    summary.total_experience_reward,
    NOW()
FROM (
    SELECT
        user_id,
        appid,
        COUNT(*) AS total_sign_days,
        COALESCE(SUM(integral_reward), 0) AS total_integral_reward,
        COALESCE(SUM(experience_reward), 0) AS total_experience_reward
    FROM daily_signins
    GROUP BY user_id, appid
) summary
JOIN LATERAL (
    SELECT sign_date, signed_at, consecutive_days
    FROM daily_signins ds
    WHERE ds.user_id = summary.user_id AND ds.appid = summary.appid
    ORDER BY sign_date DESC, id DESC
    LIMIT 1
) latest ON TRUE
ON CONFLICT (user_id, appid) DO UPDATE SET
    last_sign_date = EXCLUDED.last_sign_date,
    last_sign_at = EXCLUDED.last_sign_at,
    consecutive_days = EXCLUDED.consecutive_days,
    total_sign_days = EXCLUDED.total_sign_days,
    total_integral_reward = EXCLUDED.total_integral_reward,
    total_experience_reward = EXCLUDED.total_experience_reward,
    updated_at = NOW();

INSERT INTO sign_daily_rollups (
    appid,
    rollup_date,
    sign_user_count,
    total_integral_reward,
    total_experience_reward,
    updated_at
)
SELECT
    appid,
    sign_date,
    COUNT(*) AS sign_user_count,
    COALESCE(SUM(integral_reward), 0) AS total_integral_reward,
    COALESCE(SUM(experience_reward), 0) AS total_experience_reward,
    NOW()
FROM daily_signins
GROUP BY appid, sign_date
ON CONFLICT (appid, rollup_date) DO UPDATE SET
    sign_user_count = EXCLUDED.sign_user_count,
    total_integral_reward = EXCLUDED.total_integral_reward,
    total_experience_reward = EXCLUDED.total_experience_reward,
    updated_at = NOW();
