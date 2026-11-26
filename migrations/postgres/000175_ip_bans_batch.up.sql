-- +migrate Up
-- IP 封禁批量管理（docs/firewall.md「封禁」）：解封备注、tarpit 延迟落库（重启后同步回 Redis 不再丢）、
-- 最后修改人，以及批量筛选用到的索引。迁移器每次启动都会重跑，语句必须可重复执行。
ALTER TABLE ip_bans
    ADD COLUMN IF NOT EXISTS revoke_note TEXT   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delay_ms    INT    NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS updated_by  BIGINT;

CREATE INDEX IF NOT EXISTS idx_ip_bans_source_status  ON ip_bans (source, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ip_bans_country_status ON ip_bans (country_code, status) WHERE country_code <> '';
CREATE INDEX IF NOT EXISTS idx_ip_bans_ip_created     ON ip_bans (ip, created_at DESC);
