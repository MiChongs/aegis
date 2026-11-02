-- +migrate Up
-- 抽奖风控规则与记录检索。
--
--   lottery_draws.risk_rules    这一次命中的风控信号（trace.riskReport.hits），按风控筛记录走 GIN 索引
--   lottery_draws.risk_action   命中规则里最重的处置（damp / noPity / rare / floor），没命中为 NULL
--   lottery_risk_events         被「拒绝抽奖」挡下的请求：事务已回滚、没有抽奖记录，单独留痕
--   prize_name 三元组索引、按应用的设备 / IP / 用户索引：管理端记录搜索不再整表扫描
--
-- 迁移前命中风控的记录（trace.risk = true）回填为 legacyCluster / floor：分不清是同设备还是同网络。
-- 回填只在建 idx_lottery_draws_risk 之前跑一次（迁移器每次启动都会重跑全部 up.sql）。

ALTER TABLE lottery_draws
    ADD COLUMN IF NOT EXISTS risk_rules TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS risk_action VARCHAR(16) NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_lottery_draws_risk') THEN
        UPDATE lottery_draws SET risk_rules = ARRAY['legacyCluster'], risk_action = 'floor'
        WHERE risk_action IS NULL AND trace IS NOT NULL AND trace->>'risk' = 'true';
        CREATE INDEX idx_lottery_draws_risk ON lottery_draws (activity_id, created_at DESC) WHERE risk_action IS NOT NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_lottery_draws_app_risk
    ON lottery_draws (appid, created_at DESC) WHERE risk_action IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_lottery_draws_risk_rules
    ON lottery_draws USING gin (risk_rules) WHERE risk_action IS NOT NULL;

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_lottery_draws_prize_name_trgm
    ON lottery_draws USING gin (prize_name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_lottery_draws_app_user
    ON lottery_draws (appid, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_lottery_draws_app_device
    ON lottery_draws (appid, device_id, created_at DESC) WHERE device_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_lottery_draws_app_ip
    ON lottery_draws (appid, client_ip, created_at DESC) WHERE client_ip IS NOT NULL;

-- 风控信号「设备历史账号」：按设备数登录过的账号
CREATE INDEX IF NOT EXISTS idx_user_login_devices_device
    ON user_login_devices (appid, device_key);

CREATE TABLE IF NOT EXISTS lottery_risk_events (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    activity_id BIGINT NOT NULL REFERENCES lottery_activities(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action VARCHAR(16) NOT NULL,
    rules TEXT[] NOT NULL DEFAULT '{}',
    report JSONB NOT NULL,
    source VARCHAR(16) NOT NULL DEFAULT '',
    device_id VARCHAR(128) NULL,
    client_ip VARCHAR(64) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_lottery_risk_events_activity ON lottery_risk_events (activity_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_lottery_risk_events_app ON lottery_risk_events (appid, created_at DESC);
