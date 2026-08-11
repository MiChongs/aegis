-- +migrate Up

-- ── 订单退款汇总字段 ──
-- refunded_amount 是「已占用的退款额度」而非「已成功退款金额」：
-- 创建退款单时即预占，上游失败时释放。这样并发退款请求无法把同一笔钱退两次。
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS refunded_amount NUMERIC(18,2) NOT NULL DEFAULT 0;
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS refund_status VARCHAR(16) NOT NULL DEFAULT 'none';

-- 退款额度永远不能超过订单金额，也不能为负（并发预占的最后一道防线，由数据库兜底）
ALTER TABLE payment_orders DROP CONSTRAINT IF EXISTS ck_payment_orders_refunded_amount;
ALTER TABLE payment_orders ADD CONSTRAINT ck_payment_orders_refunded_amount
    CHECK (refunded_amount >= 0 AND refunded_amount <= amount);

CREATE INDEX IF NOT EXISTS idx_payment_orders_refund_status
    ON payment_orders(appid, refund_status) WHERE refund_status <> 'none';

-- ── 退款单 ──
CREATE TABLE IF NOT EXISTS payment_refunds (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    order_id BIGINT NOT NULL REFERENCES payment_orders(id) ON DELETE CASCADE,
    order_no VARCHAR(64) NOT NULL,
    -- 本地退款单号，同时作为提交给上游的 out_refund_no，天然幂等键
    refund_no VARCHAR(64) NOT NULL,
    provider_refund_no VARCHAR(128),
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    payment_method VARCHAR(32) NOT NULL,
    amount NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    reason TEXT,
    -- pending / processing / success / failed / closed
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    -- 履约冲正：none / done / skipped / failed
    reversal_status VARCHAR(16) NOT NULL DEFAULT 'none',
    reversal_message TEXT,
    operator VARCHAR(128),
    client_ip VARCHAR(64),
    error_message TEXT,
    raw_response JSONB NOT NULL DEFAULT '{}'::jsonb,
    refunded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_payment_refunds_no UNIQUE (refund_no)
);

CREATE INDEX IF NOT EXISTS idx_payment_refunds_order ON payment_refunds(order_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payment_refunds_app_status
    ON payment_refunds(appid, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payment_refunds_provider_no
    ON payment_refunds(provider_refund_no) WHERE provider_refund_no IS NOT NULL;
-- 未达终态的退款单：供后台补偿轮询扫描
CREATE INDEX IF NOT EXISTS idx_payment_refunds_unsettled
    ON payment_refunds(status, created_at) WHERE status IN ('pending', 'processing');
-- 冲正失败的退款单：需人工介入，单独建索引方便告警查询
CREATE INDEX IF NOT EXISTS idx_payment_refunds_reversal_failed
    ON payment_refunds(appid, created_at DESC) WHERE reversal_status = 'failed';
