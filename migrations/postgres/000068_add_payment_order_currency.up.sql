-- +migrate Up

-- ── 订单币种 ──
-- 一份支付凭证必须写明币种，而 payment_orders 此前只存了金额数值。
--
-- 币种在**下单时**按渠道配置固化，而不是开凭证时回读 payment_configs：
-- 商户随时可能把 Stripe 的计价货币从 USD 改成 EUR，配置改了不该让三个月前
-- 已经收过的那笔钱在凭证上变成另一种货币。
--
-- 历史数据留空串而不是猜一个默认值：空串在渲染时按渠道推断并明确标注为推断值，
-- 直接写死 'CNY' 会让一笔真实的美元订单在凭证上变成人民币 —— 那是伪造凭证。
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS currency VARCHAR(8) NOT NULL DEFAULT '';
