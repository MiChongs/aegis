-- +migrate Up
-- 抽奖：多维度出奖策略与全行为画像。
--
--   lottery_activities.strategy        出奖策略（软保底、匀速释放、冷却、积分预算、风控、行为画像、自定义规则），见 docs/lottery.md
--   lottery_prizes.daily_limit         全站每天最多出几个，0 为不限
--   lottery_prizes.user_limit          每人最多中几次，0 为不限
--   lottery_prizes.user_daily_limit    每人每天最多中几次，0 为不限
--   lottery_draws.trace                这一次的出奖方案（每个奖品的系数与最终概率、行为分明细）
--   lottery_prize_daily                设了每日限量的奖品当天出了几个（条件 UPSERT 保证不超发）
--   user_online_daily                  实时连接按天累计的在线秒数（行为画像的「在线时长」）
--
-- 已有活动 strategy 为空对象，奖品不限量：出奖行为与之前完全相同。
-- 迁移器每次启动都会重跑，全部语句可重复执行。

ALTER TABLE lottery_activities
    ADD COLUMN IF NOT EXISTS strategy JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE lottery_prizes
    ADD COLUMN IF NOT EXISTS daily_limit INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS user_limit INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS user_daily_limit INTEGER NOT NULL DEFAULT 0;

ALTER TABLE lottery_draws
    ADD COLUMN IF NOT EXISTS trace JSONB NULL;

CREATE TABLE IF NOT EXISTS lottery_prize_daily (
    prize_id BIGINT NOT NULL REFERENCES lottery_prizes(id) ON DELETE CASCADE,
    day_start TIMESTAMPTZ NOT NULL,
    used INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (prize_id, day_start)
);

-- 个人上限、中奖冷却、同设备 / 同网络聚集原来按记录现数，各有一个索引；000149 起改成计数
-- （lottery_user_states、lottery_risk_keys），这三个索引已删除，这里不再建。

CREATE TABLE IF NOT EXISTS user_online_daily (
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    seconds INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, day)
);
CREATE INDEX IF NOT EXISTS idx_user_online_daily_app_day ON user_online_daily (appid, day);

-- 行为画像的「近 30 天风险分」按人查
CREATE INDEX IF NOT EXISTS idx_risk_assessments_user
    ON risk_assessments (user_id, created_at DESC) WHERE user_id IS NOT NULL;
