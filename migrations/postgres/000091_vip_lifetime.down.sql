-- +migrate Down
-- 回退只回退结构：已经开通的永久会员无法还原成某个到期时间，回退前请先在控制台确认没有永久会员。

DROP INDEX IF EXISTS idx_vip_transactions_lifetime;

ALTER TABLE vip_transactions DROP CONSTRAINT IF EXISTS ck_vip_transactions_expire_after;
ALTER TABLE vip_transactions DROP COLUMN IF EXISTS lifetime;

ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS ck_vip_plans_lifetime_paid;
ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS ck_vip_plans_duration;
ALTER TABLE vip_plans DROP COLUMN IF EXISTS lifetime;
ALTER TABLE vip_plans ADD CONSTRAINT vip_plans_duration_days_check CHECK (duration_days > 0);

DROP INDEX IF EXISTS idx_users_vip_lifetime;
ALTER TABLE users DROP COLUMN IF EXISTS vip_lifetime_at;
