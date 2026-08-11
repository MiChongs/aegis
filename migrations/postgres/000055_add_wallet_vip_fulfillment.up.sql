-- +migrate Up

-- ── 用户钱包（余额系统）──
-- 每用户一行；balance 仅允许通过 wallet_transactions 账本同事务变更
CREATE TABLE IF NOT EXISTS user_wallets (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    balance NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK (balance >= 0),
    frozen NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK (frozen >= 0),
    total_recharged NUMERIC(18,2) NOT NULL DEFAULT 0,
    total_consumed NUMERIC(18,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_wallets_app ON user_wallets(appid);

-- ── 钱包流水账本 ──
-- 余额每次变动必有一条流水；idempotency_key 保证业务侧重试不重复入账
CREATE TABLE IF NOT EXISTS wallet_transactions (
    id BIGSERIAL PRIMARY KEY,
    transaction_no VARCHAR(40) NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    type VARCHAR(32) NOT NULL,
    amount NUMERIC(18,2) NOT NULL,
    balance_before NUMERIC(18,2) NOT NULL,
    balance_after NUMERIC(18,2) NOT NULL,
    related_order_no VARCHAR(64) NULL,
    idempotency_key VARCHAR(128) NULL,
    title VARCHAR(200) NOT NULL,
    remark TEXT NULL,
    operator VARCHAR(128) NULL,
    client_ip VARCHAR(64) NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_wallet_transactions_no UNIQUE (transaction_no)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_wallet_transactions_idem
    ON wallet_transactions(idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_wallet_transactions_user_time
    ON wallet_transactions(user_id, appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_wallet_transactions_order
    ON wallet_transactions(related_order_no) WHERE related_order_no IS NOT NULL;

-- ── VIP 套餐（会员系统）──
CREATE TABLE IF NOT EXISTS vip_plans (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    name VARCHAR(64) NOT NULL,
    duration_days INTEGER NOT NULL CHECK (duration_days > 0),
    price NUMERIC(18,2) NOT NULL CHECK (price >= 0),
    original_price NUMERIC(18,2) NULL,
    bonus_integral BIGINT NOT NULL DEFAULT 0 CHECK (bonus_integral >= 0),
    description TEXT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vip_plans_app_active ON vip_plans(appid, is_active, sort_order);

-- ── VIP 开通/续费记录 ──
CREATE TABLE IF NOT EXISTS vip_transactions (
    id BIGSERIAL PRIMARY KEY,
    transaction_no VARCHAR(40) NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    plan_id BIGINT NULL,
    plan_name VARCHAR(64) NOT NULL,
    duration_days INTEGER NOT NULL,
    pay_channel VARCHAR(32) NOT NULL,
    pay_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    related_order_no VARCHAR(64) NULL,
    bonus_integral BIGINT NOT NULL DEFAULT 0,
    expire_before TIMESTAMPTZ NULL,
    expire_after TIMESTAMPTZ NOT NULL,
    operator VARCHAR(128) NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_vip_transactions_no UNIQUE (transaction_no)
);

CREATE INDEX IF NOT EXISTS idx_vip_transactions_user_time
    ON vip_transactions(user_id, appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_vip_transactions_order
    ON vip_transactions(related_order_no) WHERE related_order_no IS NOT NULL;

-- ── 支付订单履约字段 ──
-- fulfillment_status: none(未履约) / done(已履约)；履约与支付状态解耦，
-- 回调重放时通过条件 UPDATE 抢占（claim）保证恰好履约一次
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS fulfillment_status VARCHAR(16) NOT NULL DEFAULT 'none';
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS fulfilled_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_payment_orders_unfulfilled
    ON payment_orders(fulfillment_status) WHERE fulfillment_status = 'none' AND status = 'paid';
