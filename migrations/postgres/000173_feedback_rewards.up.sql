-- +migrate Up
-- 有效反馈奖励积分：管理员在服务台给被采纳的意见反馈发积分，误发可撤回扣回（docs/feedback-rewards.md）。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。
--
--   feedback_reward_policies  反馈奖励策略，一个应用一行；没有这一行等于未开启（老行为不变）
--   feedback_rewards          发放记录：一张反馈同一时刻至多一条「已发放」，撤回后留档

CREATE TABLE IF NOT EXISTS feedback_reward_policies (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- 默认建议积分：分类没单独配时预填它
    default_points BIGINT NOT NULL DEFAULT 0,
    -- 按反馈分类 key 的建议积分：{"feedback_bug": 50, "feedback_feature": 30}
    category_points JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- 单条上限；每用户自然日 / 自然月（按服务器默认时区切分）累计上限。0 = 不限
    max_points BIGINT NOT NULL DEFAULT 0,
    user_daily_limit BIGINT NOT NULL DEFAULT 0,
    user_monthly_limit BIGINT NOT NULL DEFAULT 0,
    updated_by_admin_id BIGINT NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    updated_by_name VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE feedback_reward_policies DROP CONSTRAINT IF EXISTS ck_feedback_reward_policies_values;
ALTER TABLE feedback_reward_policies
    ADD CONSTRAINT ck_feedback_reward_policies_values CHECK (
        default_points >= 0 AND max_points >= 0 AND user_daily_limit >= 0 AND user_monthly_limit >= 0
    );

CREATE TABLE IF NOT EXISTS feedback_rewards (
    id BIGSERIAL PRIMARY KEY,
    ticket_id BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 用户注销后记录留档（积分流水同样留着）
    user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    -- 快照：反馈编号与分类
    ticket_no VARCHAR(40) NOT NULL DEFAULT '',
    category_key VARCHAR(64) NOT NULL DEFAULT '',
    points BIGINT NOT NULL,
    reason VARCHAR(200) NOT NULL DEFAULT '',
    -- granted 已发放 / revoked 已撤回
    status VARCHAR(16) NOT NULL DEFAULT 'granted',
    request_id VARCHAR(64) NULL,
    grant_txn_no VARCHAR(40) NOT NULL,
    granted_by_admin_id BIGINT NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    granted_by_name VARCHAR(128) NOT NULL DEFAULT '',
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 发给用户的那条站内信（撤回时一并删掉）
    notification_id BIGINT NULL,
    revoked_at TIMESTAMPTZ NULL,
    revoked_by_admin_id BIGINT NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    revoked_by_name VARCHAR(128) NOT NULL DEFAULT '',
    revoke_reason VARCHAR(200) NOT NULL DEFAULT '',
    revoke_request_id VARCHAR(64) NULL,
    revoke_txn_no VARCHAR(40) NULL,
    -- 实际扣回的积分与没扣上的差额（余额不足、选择扣到 0 时）
    clawed_back BIGINT NOT NULL DEFAULT 0,
    shortfall BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE feedback_rewards DROP CONSTRAINT IF EXISTS ck_feedback_rewards_status;
ALTER TABLE feedback_rewards
    ADD CONSTRAINT ck_feedback_rewards_status CHECK (status IN ('granted','revoked'));
ALTER TABLE feedback_rewards DROP CONSTRAINT IF EXISTS ck_feedback_rewards_points;
ALTER TABLE feedback_rewards
    ADD CONSTRAINT ck_feedback_rewards_points CHECK (points > 0 AND clawed_back >= 0 AND shortfall >= 0);

-- 一张反馈同一时刻至多一条已发放的奖励：并发、重复点击都只会落一笔
CREATE UNIQUE INDEX IF NOT EXISTS uq_feedback_rewards_active
    ON feedback_rewards(ticket_id) WHERE status = 'granted';
CREATE INDEX IF NOT EXISTS idx_feedback_rewards_ticket ON feedback_rewards(ticket_id, id DESC);
-- 每用户每日 / 每月累计上限按它数
CREATE INDEX IF NOT EXISTS idx_feedback_rewards_user_usage
    ON feedback_rewards(appid, user_id, granted_at) WHERE status = 'granted';
