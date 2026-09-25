-- +migrate Up
-- 会员权益跟随套餐：功能从「开通时快照」改为「按套餐当前配置判定」。
--
-- 000077 把功能列表按值快照进 vip_transactions.features，判定只读快照。
-- 这让套餐配置与已开通用户彻底脱钩：给高级版加一项功能老用户拿不到，
-- 把某项功能挪到更贵的档位（降级）老用户照用不误。现在的规则是：
--
--   套餐还在   → 按 vip_plans.features 的**当前**值
--   套餐被删   → 按快照（删除时会把仍在期内的记录定格成套餐最后的配置）
--   非套餐开通 → 按快照（自定义发放、卡密赠送天数，本来就只有这一份）
--
-- 并且只在启用中的功能目录里取 —— 停用或删除一个功能标识即对所有人同时生效，
-- 与服务端校验接口的结论一致。这部分是纯查询层的改动，不需要改表。
--
-- 需要改表的是另外两件「让一段会员期不再算数」的事，此前都没有落点：
--
--   退款冲正  只把 users.vip_expire_at 往回减了天数，开通记录原样留着，
--             于是退掉高级版之后，只要账上还有别的会员时长，高级版的功能照样生效。
--   扣减天数  远程函数的 vip.revoke 一直走不通（发放入口拒收负数天数）。
--
-- expire_before / expire_after 是账本，记的是开通那一刻发生了什么，不改写。
-- 会员段在链上的实际位置单独存一对字段，退款前移与扣减截断只动它们；
-- 为 NULL 表示从未被调整过，读取端按账本原值推导（因此历史数据不需要回填，
-- 滚动发布期间旧实例写入的记录也照常生效）。
--
-- 本文件可重复执行 —— 迁移运行器没有版本表，每次启动都会把所有 up.sql 重跑一遍。

ALTER TABLE vip_transactions
    ADD COLUMN IF NOT EXISTS active_from TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS active_until TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS revoke_reason VARCHAR(64) NULL;

-- 历史上已经退款冲正过的会员直购：补记作废。
-- 冲正只在全额退款时发生（部分退款记为 skipped），reversal_status = 'done' 即全额。
UPDATE vip_transactions vt
SET revoked_at = COALESCE(r.refunded_at, r.updated_at),
    revoke_reason = 'refund'
FROM payment_refunds r
JOIN payment_orders o ON o.id = r.order_id
WHERE vt.revoked_at IS NULL
  AND vt.appid = o.appid
  AND vt.related_order_no = o.order_no
  AND vt.pay_channel = 'payment_order'
  AND vt.duration_days > 0
  AND r.status = 'success'
  AND r.reversal_status = 'done'
  AND o.metadata->>'purpose' = 'vip_purchase';
