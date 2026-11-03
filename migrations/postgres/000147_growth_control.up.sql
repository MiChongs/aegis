-- +migrate Up
-- 成长调控（docs/growth-control.md）：管理员对积分与经验获取速度的奖惩控制。
--
--   growth_rate_rules   倍率规则：某个用户（user_id）或全应用（user_id 为空）× 积分 / 经验 × 分数倍率 × 每日上限 × 来源 × 生效时间
--   growth_daily_usage  有规则作用时，每人每天每类型的入账统计（每日上限按 granted_amount 判断）
--   growth_events       调控记录：建 / 改 / 撤销规则、等级封顶与解除
--   user_level_records.capped_level  等级封顶，0 = 不封顶
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS growth_rate_rules (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,
    numerator INTEGER NOT NULL,
    denominator INTEGER NOT NULL DEFAULT 1,
    rounding VARCHAR(16) NOT NULL DEFAULT 'floor',
    daily_cap BIGINT NOT NULL DEFAULT 0,
    sources TEXT[] NOT NULL DEFAULT '{}',
    starts_at TIMESTAMPTZ NULL,
    ends_at TIMESTAMPTZ NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    label VARCHAR(64) NOT NULL DEFAULT '',
    reason VARCHAR(400) NOT NULL DEFAULT '',
    created_by VARCHAR(128) NULL,
    updated_by VARCHAR(128) NULL,
    revoked_at TIMESTAMPTZ NULL,
    revoked_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_growth_rules_kind CHECK (kind IN ('integral','experience')),
    CONSTRAINT ck_growth_rules_factor CHECK (numerator BETWEEN 0 AND 1000 AND denominator BETWEEN 1 AND 1000),
    CONSTRAINT ck_growth_rules_rounding CHECK (rounding IN ('floor','round','ceil','random')),
    CONSTRAINT ck_growth_rules_cap CHECK (daily_cap >= 0),
    CONSTRAINT ck_growth_rules_window CHECK (starts_at IS NULL OR ends_at IS NULL OR ends_at > starts_at)
);
-- 入账时按 (appid, user_id 或全应用) 取未撤销的规则
CREATE INDEX IF NOT EXISTS idx_growth_rules_live ON growth_rate_rules(appid, user_id, kind) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_growth_rules_app_time ON growth_rate_rules(appid, created_at DESC);

CREATE TABLE IF NOT EXISTS growth_daily_usage (
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,
    day DATE NOT NULL,
    base_amount BIGINT NOT NULL DEFAULT 0,
    granted_amount BIGINT NOT NULL DEFAULT 0,
    suppressed_amount BIGINT NOT NULL DEFAULT 0,
    events INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id, kind, day)
);

CREATE TABLE IF NOT EXISTS growth_events (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NULL,
    rule_id BIGINT NULL,
    action VARCHAR(32) NOT NULL,
    summary VARCHAR(400) NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    operator VARCHAR(128) NULL,
    note VARCHAR(400) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_growth_events_user ON growth_events(appid, user_id, id DESC);

ALTER TABLE user_level_records ADD COLUMN IF NOT EXISTS capped_level INTEGER NOT NULL DEFAULT 0;
