-- +migrate Up
-- 注册准入的设备一侧（docs/register-ip.md）：与地址互补，同一设备注册数、地址下的不同设备、设备换的地址。
--
--   register_device_lists  设备名单：放行 / 拒绝 / 放宽，按规整后的设备标识（去空白、转小写）精确匹配
--   register_ip_ledger     加按设备的索引：累计型「同一设备」规则与设备查询、关联视图按它数
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS register_device_lists (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    device_id VARCHAR(128) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    factor DOUBLE PRECISION NOT NULL DEFAULT 1,
    note VARCHAR(200) NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NULL,
    hits BIGINT NOT NULL DEFAULT 0,
    last_hit_at TIMESTAMPTZ NULL,
    created_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_register_device_lists UNIQUE (appid, device_id)
);
ALTER TABLE register_device_lists DROP CONSTRAINT IF EXISTS ck_register_device_lists_kind;
ALTER TABLE register_device_lists ADD CONSTRAINT ck_register_device_lists_kind
    CHECK (kind IN ('allow', 'deny', 'elevated'));

-- 台账里的设备标识统一按规整后的形态比对（与服务端 regip.ParseDevice 一致）
CREATE INDEX IF NOT EXISTS idx_register_ip_ledger_device
    ON register_ip_ledger (appid, lower(device_id)) WHERE device_id <> '' AND released_at IS NULL;
-- 关联视图：设备用过哪些地址、地址下出现过哪些设备（含已解除）
CREATE INDEX IF NOT EXISTS idx_register_ip_ledger_device_all
    ON register_ip_ledger (appid, lower(device_id), created_at DESC) WHERE device_id <> '';
CREATE INDEX IF NOT EXISTS idx_register_ip_events_device
    ON register_ip_events (appid, lower(device_id), created_at DESC) WHERE device_id <> '';
