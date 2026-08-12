-- +migrate Up
-- 试用期会员。
--
-- 在此之前，「会员」在库里只有一个事实：users.vip_expire_at 是不是还没到。
-- 付费买的、管理员送的、活动送的，落库后长得一模一样 —— 于是三件事做不到：
--
--   1. 客户端分不出「试用中」和「已付费」，两种状态该显示的按钮正好相反
--      （前者是"立即升级"，后者是"续费"）；
--   2. 「试用只能领一次」无从判断 —— 没有任何一行记录说得出谁领过；
--   3. 运营算不出转化率，而那正是开试用的理由。
--
-- 因此试用**不另建一套时长体系**：它仍然只是把 vip_expire_at 往后推，仍然进
-- vip_transactions 账本（pay_channel = 'trial'）。新增的只是「谁领过、领的是哪个套餐、
-- 领到什么时候」这份资格账本。

-- ── 会员套餐：区分「付费」与「试用」 ──
--
-- 试用做成套餐的一种，而不是 apps.settings 里的一组开关：时长 / 名称 / 描述 /
-- 赠送积分 / 上下架 / 排序，套餐已经全都有。分两处配置会立刻产生
-- 「控制台显示 7 天、实际发 14 天」这类没人能一眼看穿的漂移。
ALTER TABLE vip_plans
    ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'paid',
    ADD COLUMN IF NOT EXISTS trial_device_limited BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS ck_vip_plans_kind;
ALTER TABLE vip_plans ADD CONSTRAINT ck_vip_plans_kind CHECK (kind IN ('paid', 'trial'));

-- 试用套餐恒为 0 元。定价大于 0 的试用意味着它同时出现在"领取"和"购买"两条链路上，
-- 而领取链路不收钱 —— 那就是一个可以反复触发的免费入口。
ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS ck_vip_plans_trial_free;
ALTER TABLE vip_plans ADD CONSTRAINT ck_vip_plans_trial_free CHECK (kind <> 'trial' OR price = 0);

-- 每个应用至多一个「启用中」的试用套餐：多于一个时「点领取到底领哪个」没有答案，
-- 而这个答案不该由 ORDER BY 的偶然顺序决定。
CREATE UNIQUE INDEX IF NOT EXISTS uq_vip_plans_active_trial
    ON vip_plans(appid) WHERE kind = 'trial' AND is_active;

-- ── 试用领取记录（资格账本）──
--
-- 一人一次由唯一约束保证，不靠应用层判断：判断会被并发穿透，约束不会。
CREATE TABLE IF NOT EXISTS vip_trial_claims (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id BIGINT NULL,
    plan_name VARCHAR(64) NOT NULL,
    duration_days INTEGER NOT NULL CHECK (duration_days > 0),
    -- 这次试用发到什么时候。判「当前是不是试用中」靠它与 users.vip_expire_at 比对：
    -- 两者相等即"这段会员期就是试用给的"；用户后来买了付费，到期时间会被推远，
    -- 于是自动不再算试用 —— 不需要任何额外的状态迁移。
    trial_ends_at TIMESTAMPTZ NOT NULL,
    transaction_no VARCHAR(40) NOT NULL,
    device_id VARCHAR(128) NULL,
    -- 领取当时是否开着设备维度去重。规则是可以改的，而已经发生的领取不该被追溯改判，
    -- 因此把当时的规则记在行上，唯一索引也据此收敛。
    device_locked BOOLEAN NOT NULL DEFAULT FALSE,
    client_ip VARCHAR(64) NULL,
    -- 管理员代领 / 重置后重领时留痕，用户自助领取为空
    operator VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_vip_trial_claims_user UNIQUE (appid, user_id)
);

CREATE INDEX IF NOT EXISTS idx_vip_trial_claims_app_time
    ON vip_trial_claims(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_vip_trial_claims_device
    ON vip_trial_claims(appid, device_id) WHERE device_id IS NOT NULL;

-- 设备维度去重开着时，由数据库兜住「两个不同账号在同一台设备上同时领取」——
-- 那两条请求锁的是不同的用户行，应用层的 SELECT 检查拦不住它们。
CREATE UNIQUE INDEX IF NOT EXISTS uq_vip_trial_claims_device
    ON vip_trial_claims(appid, device_id) WHERE device_locked AND device_id IS NOT NULL;
