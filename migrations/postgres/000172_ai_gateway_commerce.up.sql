-- +migrate Up
-- AI 额度独立成资产：时段价格、会员档位优惠、额度包商店、限期额度批次、新人礼（docs/ai-billing.md）。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。
--
--   ai_gateway_packages          额度包（管理员上架，用户用钱包余额购买）
--   ai_gateway_package_orders    额度包订单（价格、折扣、实得额度的快照，退款）
--   ai_gateway_credit_lots       额度批次：买来的、新人礼、管理员限期发放的额度，先到期的先用
--   ai_gateway_credit_ledger     额度流水，新增 bucket（credit 永久 / pack 额度包）、lot_id、total_after
--   ai_gateway_accounts          新增 welcome_claimed_at（新人礼每人一次）
--
-- 金额：额度以微点存（1 点 = 10⁶ 微点），钱以元存 NUMERIC(12,2)。

CREATE TABLE IF NOT EXISTS ai_gateway_packages (
    id              BIGSERIAL PRIMARY KEY,
    appid           BIGINT        NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name            VARCHAR(32)   NOT NULL,
    subtitle        VARCHAR(64)   NOT NULL DEFAULT '',
    description     TEXT          NOT NULL DEFAULT '',
    badge           VARCHAR(16)   NOT NULL DEFAULT '',
    tone            VARCHAR(16)   NOT NULL DEFAULT 'blue',
    -- 额度与赠送额度（微点），进同一个批次、同一到期时间
    points          BIGINT        NOT NULL,
    bonus_points    BIGINT        NOT NULL DEFAULT 0,
    price           NUMERIC(12,2) NOT NULL DEFAULT 0,
    original_price  NUMERIC(12,2) NOT NULL DEFAULT 0,
    -- 有效天数，0 为永久
    validity_days   INT           NOT NULL DEFAULT 0,
    audience        VARCHAR(8)    NOT NULL DEFAULT 'all',
    tier_ids        BIGINT[]      NOT NULL DEFAULT '{}',
    member_discount BOOLEAN       NOT NULL DEFAULT TRUE,
    per_user_limit  INT           NOT NULL DEFAULT 0,
    monthly_limit   INT           NOT NULL DEFAULT 0,
    stock           INT           NOT NULL DEFAULT 0,
    sold            INT           NOT NULL DEFAULT 0,
    starts_at       TIMESTAMPTZ,
    ends_at         TIMESTAMPTZ,
    featured        BOOLEAN       NOT NULL DEFAULT FALSE,
    enabled         BOOLEAN       NOT NULL DEFAULT TRUE,
    sort_order      INT           NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_ai_gateway_packages_amounts CHECK (
        points > 0 AND bonus_points >= 0 AND price >= 0 AND original_price >= 0 AND validity_days >= 0 AND
        per_user_limit >= 0 AND monthly_limit >= 0 AND stock >= 0 AND sold >= 0),
    CONSTRAINT ck_ai_gateway_packages_audience CHECK (audience IN ('all', 'vip'))
);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_packages_app ON ai_gateway_packages (appid, sort_order, id);

CREATE TABLE IF NOT EXISTS ai_gateway_credit_lots (
    id         BIGSERIAL PRIMARY KEY,
    appid      BIGINT       NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id    BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- purchase 购买 / welcome 新人礼 / admin 管理员发放 / grant 发放规则与活动
    origin     VARCHAR(16)  NOT NULL,
    title      VARCHAR(64)  NOT NULL DEFAULT '',
    amount     BIGINT       NOT NULL,
    remaining  BIGINT       NOT NULL,
    -- 调用暂扣中的部分（结算后放回 remaining 或消耗掉）
    frozen     BIGINT       NOT NULL DEFAULT 0,
    order_no   VARCHAR(40),
    package_id BIGINT,
    expires_at TIMESTAMPTZ,
    -- active / expired（到期清零）/ revoked（作废、退款）
    status     VARCHAR(16)  NOT NULL DEFAULT 'active',
    request_id VARCHAR(64),
    operator   VARCHAR(64),
    remark     VARCHAR(200) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_ai_gateway_credit_lots_origin CHECK (origin IN ('purchase', 'welcome', 'admin', 'grant')),
    CONSTRAINT ck_ai_gateway_credit_lots_status CHECK (status IN ('active', 'expired', 'revoked')),
    CONSTRAINT ck_ai_gateway_credit_lots_amounts CHECK (
        amount > 0 AND remaining >= 0 AND frozen >= 0 AND remaining + frozen <= amount)
);

-- 暂扣按到期时间取可用批次；后台按到期时间清理
CREATE INDEX IF NOT EXISTS idx_ai_gateway_credit_lots_user
    ON ai_gateway_credit_lots (user_id, status, expires_at, id);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_credit_lots_app ON ai_gateway_credit_lots (appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_credit_lots_sweep
    ON ai_gateway_credit_lots (expires_at) WHERE status = 'active' AND expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ai_gateway_credit_lots_leftover
    ON ai_gateway_credit_lots (id) WHERE status <> 'active' AND remaining > 0;
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_gateway_credit_lots_request
    ON ai_gateway_credit_lots (appid, request_id) WHERE request_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS ai_gateway_package_orders (
    id             BIGSERIAL PRIMARY KEY,
    order_no       VARCHAR(40)   NOT NULL,
    appid          BIGINT        NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id        BIGINT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    package_id     BIGINT        NOT NULL,
    -- 下单那一刻的快照：包改了名、改了价，订单说的仍是当时买的那一份
    package_name   VARCHAR(32)   NOT NULL,
    list_price     NUMERIC(12,2) NOT NULL,
    price          NUMERIC(12,2) NOT NULL,
    discount_label VARCHAR(64)   NOT NULL DEFAULT '',
    points         BIGINT        NOT NULL,
    -- 包自带赠送 + 会员多送
    bonus_points   BIGINT        NOT NULL DEFAULT 0,
    validity_days  INT           NOT NULL DEFAULT 0,
    expires_at     TIMESTAMPTZ,
    lot_id         BIGINT,
    wallet_txn_no  VARCHAR(64),
    status         VARCHAR(16)   NOT NULL DEFAULT 'paid',
    refund_amount  NUMERIC(12,2) NOT NULL DEFAULT 0,
    refund_points  BIGINT        NOT NULL DEFAULT 0,
    refund_reason  VARCHAR(200),
    refunded_by    VARCHAR(64),
    refunded_at    TIMESTAMPTZ,
    request_id     VARCHAR(64)   NOT NULL,
    client_ip      VARCHAR(64),
    created_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_ai_gateway_package_orders_status CHECK (status IN ('paid', 'refunded')),
    CONSTRAINT ck_ai_gateway_package_orders_amounts CHECK (
        list_price >= 0 AND price >= 0 AND points >= 0 AND bonus_points >= 0 AND refund_amount >= 0 AND refund_points >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_gateway_package_orders_no ON ai_gateway_package_orders (order_no);
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_gateway_package_orders_request ON ai_gateway_package_orders (user_id, request_id);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_package_orders_app ON ai_gateway_package_orders (appid, created_at DESC);
-- 限购计数：某人某个包的有效订单
CREATE INDEX IF NOT EXISTS idx_ai_gateway_package_orders_user
    ON ai_gateway_package_orders (user_id, package_id, created_at) WHERE status = 'paid';

ALTER TABLE ai_gateway_accounts ADD COLUMN IF NOT EXISTS welcome_claimed_at TIMESTAMPTZ;

ALTER TABLE ai_gateway_credit_ledger ADD COLUMN IF NOT EXISTS bucket VARCHAR(8) NOT NULL DEFAULT 'credit';
ALTER TABLE ai_gateway_credit_ledger ADD COLUMN IF NOT EXISTS lot_id BIGINT;
-- 变动后的 AI 额度合计（永久 + 额度包）；之前的行没有，读的时候退回 balance_after
ALTER TABLE ai_gateway_credit_ledger ADD COLUMN IF NOT EXISTS total_after BIGINT;
ALTER TABLE ai_gateway_credit_ledger DROP CONSTRAINT IF EXISTS ck_ai_gateway_credit_ledger_kind;
ALTER TABLE ai_gateway_credit_ledger ADD CONSTRAINT ck_ai_gateway_credit_ledger_kind CHECK (kind IN (
    'grant', 'consume', 'refund', 'adjust', 'purchase', 'welcome', 'expire', 'revoke', 'order_refund'));
ALTER TABLE ai_gateway_credit_ledger DROP CONSTRAINT IF EXISTS ck_ai_gateway_credit_ledger_bucket;
ALTER TABLE ai_gateway_credit_ledger ADD CONSTRAINT ck_ai_gateway_credit_ledger_bucket CHECK (bucket IN ('credit', 'pack'));

-- 扣费来源去掉积分（AI 额度与积分不再互通），并在永久额度之前补上额度包。
-- 只处理还带着 integral 的设置：跑过一次之后 integral 就没了，重放不会再动管理员后来的排序。
UPDATE ai_gateway_settings
SET settings = jsonb_set(settings, '{sources}', (
        SELECT COALESCE(jsonb_agg(to_jsonb(x.value) ORDER BY x.pos), '[]'::jsonb)
        FROM (
            SELECT e.value, e.ord * 2 AS pos
            FROM jsonb_array_elements_text(settings -> 'sources') WITH ORDINALITY AS e(value, ord)
            WHERE e.value <> 'integral'
            UNION ALL
            SELECT 'pack', COALESCE((
                SELECT c.ord * 2 - 1
                FROM jsonb_array_elements_text(settings -> 'sources') WITH ORDINALITY AS c(value, ord)
                WHERE c.value = 'credit' LIMIT 1), 1000000)
            WHERE NOT (settings -> 'sources' ? 'pack')
        ) x
    )),
    updated_at = NOW()
WHERE jsonb_typeof(settings -> 'sources') = 'array' AND settings -> 'sources' ? 'integral';
