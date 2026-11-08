-- +migrate Up
-- 支付订单的实付金额与「待人工处理」（docs/payment-review.md）。
--
-- 爱发电的自选金额付款页上，付款人可以改金额：2026-10-14 至 11-08 有 8 笔永久会员订单（49.90 / 49.99），
-- 付款人改成了 50.00 或 49.00。结单要求实付等于订单金额，于是全部被拒、订单过期，管理员逐笔手动补发；
-- 被拒的订单在 24 小时内每 2 分钟查一次单，每次都写一条回调日志（一笔就是两百多条）。
--
-- 现在：
--   paid_amount    渠道报的实付金额（结单时记下；与 amount 不同时就是多付或少付）
--   review_reason  需要人工处理的原因（amount_mismatch：实付少于订单金额，或不允许改金额的渠道金额对不上）；
--                  有值的订单不再自动查单补单，管理员收到一条站内通知
--   review_note / review_at  说明与标记时间
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS paid_amount NUMERIC(18, 2) NULL,
    ADD COLUMN IF NOT EXISTS review_reason VARCHAR(32) NULL,
    ADD COLUMN IF NOT EXISTS review_note TEXT NULL,
    ADD COLUMN IF NOT EXISTS review_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS idx_payment_orders_review ON payment_orders(appid, review_at DESC) WHERE review_reason IS NOT NULL;

-- 已经因金额不一致被拒过的未结订单：补记实付金额与原因，从此不再查单重试。
-- 这些订单当时都由管理员手动补发了会员，所以只补记、不发通知。
UPDATE payment_orders o SET
    review_reason = 'amount_mismatch',
    review_note = '000150 补记：当时实付金额与订单金额不一致、未自动结单（此前已由管理员手动处理）',
    review_at = NOW(),
    paid_amount = (
        SELECT (l.callback_data->'order'->>'total_amount')::numeric FROM payment_callback_logs l
        WHERE l.order_id = o.id AND l.verification_status = 'amount_mismatch'
          AND l.callback_data->'order'->>'total_amount' ~ '^[0-9]+(\.[0-9]+)?$'
        ORDER BY l.id DESC LIMIT 1)
WHERE o.status <> 'paid' AND o.review_reason IS NULL
  AND EXISTS (SELECT 1 FROM payment_callback_logs l WHERE l.order_id = o.id AND l.verification_status = 'amount_mismatch');
