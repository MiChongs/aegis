-- +migrate Up
-- 优惠系统：优惠券（模板 → 用户券）、优惠活动（限时折扣 / 满减 / 支付返券返积分）与订单上的优惠记录（docs/coupons.md）。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。
--
--   coupon_templates          券的规则：方式、面额、门槛、范围、有效期、库存、领券中心设置
--   user_coupons              用户手里的每一张券：可用 → 占用（待支付订单）→ 已使用；或过期、撤销
--   coupon_promotions         优惠活动
--   payment_order_promotions  一笔订单用了哪个活动、哪张券、命中了哪个返券活动；预占 → 结算 / 释放 / 退款

CREATE TABLE IF NOT EXISTS coupon_templates (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,
    description VARCHAR(400) NOT NULL DEFAULT '',
    theme VARCHAR(16) NOT NULL DEFAULT 'red',
    kind VARCHAR(16) NOT NULL,
    amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    rate INT NOT NULL DEFAULT 0,
    max_discount NUMERIC(12,2) NOT NULL DEFAULT 0,
    min_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    plan_ids BIGINT[] NOT NULL DEFAULT '{}',
    stack_with_promotion BOOLEAN NOT NULL DEFAULT FALSE,
    validity_mode VARCHAR(16) NOT NULL,
    valid_from TIMESTAMPTZ NULL,
    valid_until TIMESTAMPTZ NULL,
    valid_days INT NOT NULL DEFAULT 0,
    valid_delay_days INT NOT NULL DEFAULT 0,
    -- 库存：issued_count 在发券的事务里条件自增（不超过 total_quantity），全部渠道共用
    total_quantity BIGINT NOT NULL DEFAULT 0,
    issued_count BIGINT NOT NULL DEFAULT 0,
    used_count BIGINT NOT NULL DEFAULT 0,
    discount_total NUMERIC(14,2) NOT NULL DEFAULT 0,
    per_user_limit INT NOT NULL DEFAULT 0,
    claimable BOOLEAN NOT NULL DEFAULT FALSE,
    claim_starts_at TIMESTAMPTZ NULL,
    claim_ends_at TIMESTAMPTZ NULL,
    audience JSONB NOT NULL DEFAULT '{"kind":"all"}'::jsonb,
    refund_return BOOLEAN NOT NULL DEFAULT TRUE,
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    created_by VARCHAR(128) NULL,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_coupon_templates_kind CHECK (kind IN ('amount_off','percent_off')),
    CONSTRAINT ck_coupon_templates_status CHECK (status IN ('draft','active','paused','ended')),
    CONSTRAINT ck_coupon_templates_validity CHECK (validity_mode IN ('fixed','relative')),
    CONSTRAINT ck_coupon_templates_stock CHECK (total_quantity = 0 OR issued_count <= total_quantity)
);
CREATE INDEX IF NOT EXISTS idx_coupon_templates_app ON coupon_templates(appid, status, id DESC);

CREATE TABLE IF NOT EXISTS user_coupons (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    template_id BIGINT NOT NULL REFERENCES coupon_templates(id) ON DELETE RESTRICT,
    -- 券号：展示与客服查询用，应用内唯一
    code VARCHAR(24) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'available',
    source VARCHAR(24) NOT NULL,
    source_ref VARCHAR(96) NOT NULL DEFAULT '',
    valid_from TIMESTAMPTZ NOT NULL,
    valid_until TIMESTAMPTZ NOT NULL,
    -- 占用（locked）或使用（used）它的订单
    order_no VARCHAR(64) NULL,
    used_at TIMESTAMPTZ NULL,
    discount NUMERIC(12,2) NOT NULL DEFAULT 0,
    revoked_reason VARCHAR(200) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_user_coupons_status CHECK (status IN ('available','locked','used','expired','revoked')),
    CONSTRAINT ck_user_coupons_order CHECK (status NOT IN ('locked','used') OR order_no IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_user_coupons_code ON user_coupons(appid, code);
CREATE INDEX IF NOT EXISTS idx_user_coupons_user ON user_coupons(appid, user_id, status, valid_until);
CREATE INDEX IF NOT EXISTS idx_user_coupons_template ON user_coupons(template_id, user_id, source);
CREATE INDEX IF NOT EXISTS idx_user_coupons_order ON user_coupons(order_no) WHERE order_no IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_user_coupons_app_created ON user_coupons(appid, created_at DESC);
-- 过期扫描
CREATE INDEX IF NOT EXISTS idx_user_coupons_expiring ON user_coupons(valid_until) WHERE status = 'available';

CREATE TABLE IF NOT EXISTS coupon_promotions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,
    description VARCHAR(400) NOT NULL DEFAULT '',
    tag VARCHAR(32) NOT NULL DEFAULT '',
    theme VARCHAR(16) NOT NULL DEFAULT 'red',
    kind VARCHAR(16) NOT NULL,
    rule JSONB NOT NULL DEFAULT '{}'::jsonb,
    priority INT NOT NULL DEFAULT 0,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    plan_ids BIGINT[] NOT NULL DEFAULT '{}',
    package_ids BIGINT[] NOT NULL DEFAULT '{}',
    audience JSONB NOT NULL DEFAULT '{"kind":"all"}'::jsonb,
    stack_with_coupon BOOLEAN NOT NULL DEFAULT FALSE,
    starts_at TIMESTAMPTZ NULL,
    ends_at TIMESTAMPTZ NULL,
    per_user_limit INT NOT NULL DEFAULT 0,
    total_limit BIGINT NOT NULL DEFAULT 0,
    budget NUMERIC(14,2) NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    -- 名额与预算在下单事务里条件预占，订单关闭 / 全额退款时退回
    used_count BIGINT NOT NULL DEFAULT 0,
    discount_total NUMERIC(14,2) NOT NULL DEFAULT 0,
    created_by VARCHAR(128) NULL,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_coupon_promotions_kind CHECK (kind IN ('discount','threshold','rebate')),
    -- 名额与预算不用 CHECK：下单时的条件预占保证不超；订单关闭后才到账的付款要重新计入（钱已经收了），
    -- 那一刻可能刚好超一点，不能因此让结单失败
    CONSTRAINT ck_coupon_promotions_status CHECK (status IN ('draft','active','paused','ended'))
);
CREATE INDEX IF NOT EXISTS idx_coupon_promotions_app ON coupon_promotions(appid, status, id DESC);

CREATE TABLE IF NOT EXISTS payment_order_promotions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id BIGINT NOT NULL REFERENCES payment_orders(id) ON DELETE CASCADE,
    order_no VARCHAR(64) NOT NULL,
    purpose VARCHAR(32) NOT NULL DEFAULT '',
    original_amount NUMERIC(12,2) NOT NULL,
    promotion_discount NUMERIC(12,2) NOT NULL DEFAULT 0,
    coupon_discount NUMERIC(12,2) NOT NULL DEFAULT 0,
    payable_amount NUMERIC(12,2) NOT NULL,
    promotion_id BIGINT NULL REFERENCES coupon_promotions(id) ON DELETE SET NULL,
    promotion_name VARCHAR(64) NOT NULL DEFAULT '',
    user_coupon_id BIGINT NULL REFERENCES user_coupons(id) ON DELETE SET NULL,
    template_id BIGINT NULL REFERENCES coupon_templates(id) ON DELETE SET NULL,
    coupon_name VARCHAR(64) NOT NULL DEFAULT '',
    rebate_promotion_id BIGINT NULL REFERENCES coupon_promotions(id) ON DELETE SET NULL,
    rebate_promotion_name VARCHAR(64) NOT NULL DEFAULT '',
    rebate_status VARCHAR(16) NOT NULL DEFAULT 'none',
    rebate_note VARCHAR(400) NOT NULL DEFAULT '',
    -- 返出去的券与积分（退款时按它收回）
    rebate_coupon_ids BIGINT[] NOT NULL DEFAULT '{}',
    rebate_integral BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'reserved',
    note VARCHAR(400) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at TIMESTAMPTZ NULL,
    released_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_payment_order_promotions_status CHECK (status IN ('reserved','settled','released','refunded')),
    CONSTRAINT ck_payment_order_promotions_rebate CHECK (rebate_status IN ('none','pending','granted','skipped','revoked','failed'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_payment_order_promotions_order ON payment_order_promotions(order_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_payment_order_promotions_order_no ON payment_order_promotions(order_no);
CREATE INDEX IF NOT EXISTS idx_payment_order_promotions_app ON payment_order_promotions(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payment_order_promotions_promo ON payment_order_promotions(promotion_id, user_id, status) WHERE promotion_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_payment_order_promotions_rebate ON payment_order_promotions(rebate_promotion_id, user_id, status) WHERE rebate_promotion_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_payment_order_promotions_template ON payment_order_promotions(template_id) WHERE template_id IS NOT NULL;
-- 释放扫描：还在预占中的
CREATE INDEX IF NOT EXISTS idx_payment_order_promotions_reserved ON payment_order_promotions(created_at) WHERE status = 'reserved';
