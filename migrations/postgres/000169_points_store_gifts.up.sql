-- +migrate Up
-- 积分商城「赠好友」：兑换时直接选好友，扣积分与给对方发放在同一个事务里完成（docs/points-store-gift.md）。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。
--
--   points_store_items.delivery     发给谁：self 兑换人自己 / gift 兑换人选的好友
--   points_store_gifts              赠送记录：送达、审核中、驳回、撤销，以及被风控拦下的尝试
--   points_store_gift_policies      赠送风控策略，一个应用一行；没有这一行按推荐默认值

ALTER TABLE points_store_items ADD COLUMN IF NOT EXISTS delivery VARCHAR(8) NOT NULL DEFAULT 'self';
ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_delivery;
ALTER TABLE points_store_items
    ADD CONSTRAINT ck_points_store_items_delivery CHECK (delivery IN ('self','gift'));

-- 撤销追回之后、没有退回积分的那笔兑换记为 revoked（积分已花出去，权益已收回）
ALTER TABLE points_store_redemptions DROP CONSTRAINT IF EXISTS ck_points_store_redemptions_status;
ALTER TABLE points_store_redemptions
    ADD CONSTRAINT ck_points_store_redemptions_status CHECK (status IN ('completed','pending','cancelled','revoked'));

CREATE TABLE IF NOT EXISTS points_store_gifts (
    id BIGSERIAL PRIMARY KEY,
    gift_no VARCHAR(40) NOT NULL,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 被风控拦下的尝试没有兑换记录（没扣积分）
    redemption_id BIGINT NULL REFERENCES points_store_redemptions(id) ON DELETE SET NULL,
    sender_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    recipient_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    item_id BIGINT NULL REFERENCES points_store_items(id) ON DELETE SET NULL,
    -- 快照：商品改了、删了，记录说的仍是当时送的那一份
    item_title VARCHAR(64) NOT NULL,
    cost BIGINT NOT NULL DEFAULT 0,
    rewards JSONB NOT NULL DEFAULT '[]'::jsonb,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    message VARCHAR(200) NOT NULL DEFAULT '',
    -- delivered 已送达 / reviewing 审核中（积分已扣、未发放）/ rejected 已驳回（积分已退）/
    -- revoked 已撤销（权益已追回）/ blocked 风控拦截（未扣积分）
    status VARCHAR(16) NOT NULL,
    -- 风控结论：allow / review / reject，与命中的判据（给管理员看）
    risk_action VARCHAR(16) NOT NULL DEFAULT 'allow',
    risk_hits JSONB NOT NULL DEFAULT '[]'::jsonb,
    risk_decision_id VARCHAR(64) NULL,
    -- 判定时的事实快照（注册天数、共用设备与地址、当日次数……）
    risk_facts JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_id VARCHAR(64) NULL,
    device_id VARCHAR(128) NULL,
    client_ip VARCHAR(64) NULL,
    review_note TEXT NULL,
    reviewed_by VARCHAR(128) NULL,
    reviewed_at TIMESTAMPTZ NULL,
    revoke_note TEXT NULL,
    revoked_by VARCHAR(128) NULL,
    revoked_at TIMESTAMPTZ NULL,
    -- 逐项追回的结果
    revocation JSONB NULL,
    refund_transaction_no VARCHAR(64) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_points_store_gifts_status CHECK (status IN ('delivered','reviewing','rejected','revoked','blocked')),
    CONSTRAINT ck_points_store_gifts_action CHECK (risk_action IN ('allow','review','reject'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_points_store_gifts_no ON points_store_gifts(gift_no);
CREATE UNIQUE INDEX IF NOT EXISTS uq_points_store_gifts_redemption
    ON points_store_gifts(redemption_id) WHERE redemption_id IS NOT NULL;
-- 送出的 / 收到的：我的赠送记录，以及风控的当日次数、同一对人的频次
CREATE INDEX IF NOT EXISTS idx_points_store_gifts_sender ON points_store_gifts(appid, sender_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_points_store_gifts_recipient ON points_store_gifts(appid, recipient_id, created_at DESC);
-- 管理端：审核队列与按状态筛选
CREATE INDEX IF NOT EXISTS idx_points_store_gifts_status ON points_store_gifts(appid, status, created_at DESC);

CREATE TABLE IF NOT EXISTS points_store_gift_policies (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by VARCHAR(128) NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
