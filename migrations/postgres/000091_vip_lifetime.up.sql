-- +migrate Up
-- 永久会员。
--
-- 此前会员只有一种形状：users.vip_expire_at 往后推。老系统里的永久会员（vip_time = 999999999）
-- 迁进来时只能被映射成 2099-12-31 23:59:59 —— 一个假装是日期的标记。它带来三件说不清的事：
--
--   1. 客户端显示「有效期至 2099-12-31，剩余 26000 天」；
--   2. 永久会员再续一个月，到期时间变成 2100 年，钱照扣；
--   3. 运营想卖「永久套餐」只能填 36500 天，退款冲正时按天数倒扣，扣完人还在 2099 年。
--
-- 所以永久做成与限时**并列**的一条线，而不是一个很远的到期时间：
--
--   users.vip_lifetime_at        成为永久会员的时间，非空即永久会员（判定的权威来源）
--   vip_plans.lifetime           永久套餐：不带时长，开通一次永久生效
--   vip_transactions.lifetime    这笔开通是永久的：它是一段没有终点的会员期，不参与顺延、
--                                退款前移与扣减截断；expire_after 为空（没有到期时间）
--
-- 限时那条线（vip_expire_at + 各段）原样保留：永久会员另买一段限时的高级版，
-- 高级版那段照常顺延、照常到期，功能取两条线上仍生效各段的并集。
--
-- 本文件可重复执行 —— 迁移运行器没有版本表，每次启动都会把所有 up.sql 重跑一遍。

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS vip_lifetime_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_users_vip_lifetime
    ON users(appid) WHERE vip_lifetime_at IS NOT NULL;

-- ── 永久套餐 ──
ALTER TABLE vip_plans
    ADD COLUMN IF NOT EXISTS lifetime BOOLEAN NOT NULL DEFAULT FALSE;

-- 时长约束改为「永久套餐恒为 0 天，限时套餐必须大于 0 天」。
-- 原约束写在 000055 的建表语句里（自动命名为 vip_plans_duration_days_check），
-- 那份文件是 CREATE TABLE IF NOT EXISTS，重跑时不会把它加回来。
ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS vip_plans_duration_days_check;
ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS ck_vip_plans_duration;
ALTER TABLE vip_plans ADD CONSTRAINT ck_vip_plans_duration
    CHECK (CASE WHEN lifetime THEN duration_days = 0 ELSE duration_days > 0 END);

-- 试用是资格制的「先体验一段」，永久的试用就是白送永久会员。
ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS ck_vip_plans_lifetime_paid;
ALTER TABLE vip_plans ADD CONSTRAINT ck_vip_plans_lifetime_paid
    CHECK (NOT lifetime OR kind = 'paid');

-- ── 永久开通 ──
ALTER TABLE vip_transactions
    ADD COLUMN IF NOT EXISTS lifetime BOOLEAN NOT NULL DEFAULT FALSE;

-- 永久开通没有到期时间。限时记录仍然必须有 —— 会员段的终点按它推导。
ALTER TABLE vip_transactions ALTER COLUMN expire_after DROP NOT NULL;
ALTER TABLE vip_transactions DROP CONSTRAINT IF EXISTS ck_vip_transactions_expire_after;
ALTER TABLE vip_transactions ADD CONSTRAINT ck_vip_transactions_expire_after
    CHECK (lifetime OR expire_after IS NOT NULL);

-- 判定热路径：某用户仍生效的永久开通（取功能并集、退款后重算永久身份）
CREATE INDEX IF NOT EXISTS idx_vip_transactions_lifetime
    ON vip_transactions(appid, user_id) WHERE lifetime AND revoked_at IS NULL;

-- ── 老系统的永久会员 ──
--
-- 导入时被映射成 2099-12-31 23:59:59 UTC（见 normalizeLegacyVIPTime）。这里把它们转成真正的永久会员：
-- 补一笔永久开通记录（渠道 legacy_import，判定为来源不明 —— 老系统没有留下凭什么），
-- 清掉那个假的到期时间。
--
-- 按 >= 而不是 = 匹配：被映射之后又续过天数的，到期时间会落在 2099 年之后，同样是永久会员。
-- 已有仍生效的永久开通的不再补记，因此重跑不会重复记账；重新导入又被写回 2099 的，这里会再收敛一次。
WITH legacy AS (
    SELECT id, appid, vip_expire_at
    FROM users
    WHERE vip_expire_at >= TIMESTAMPTZ '2099-12-31 23:59:59+00'
), ledger AS (
    INSERT INTO vip_transactions (transaction_no, user_id, appid, plan_id, plan_name, features, duration_days,
        pay_channel, pay_amount, related_order_no, bonus_integral, expire_before, expire_after, operator, metadata,
        active_from, active_until, lifetime, created_at)
    SELECT 'VIPL' || l.id::text || '-' || floor(extract(epoch FROM NOW()))::bigint::text,
           l.id, l.appid, NULL, '永久会员', '{}', 0,
           'legacy_import', 0, NULL, 0, l.vip_expire_at, NULL, 'system:legacy-import',
           jsonb_build_object('legacyVipExpireAt', l.vip_expire_at),
           NOW(), NULL, TRUE, NOW()
    FROM legacy l
    WHERE NOT EXISTS (
        SELECT 1 FROM vip_transactions t
        WHERE t.appid = l.appid AND t.user_id = l.id AND t.lifetime
          AND t.revoked_at IS NULL AND t.pay_channel <> 'admin_revoke'
    )
    ON CONFLICT (transaction_no) DO NOTHING
    RETURNING user_id
)
UPDATE users u
SET vip_lifetime_at = COALESCE(u.vip_lifetime_at, NOW()),
    vip_expire_at = NULL,
    updated_at = NOW()
FROM legacy l
WHERE u.id = l.id;
