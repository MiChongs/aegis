-- +migrate Up
-- 注册 IP 控制。策略存在 apps.settings.registerIp，规则见 docs/register-ip.md。
--
--   register_ip_ledger  注册台账：每个新账号一行，记下注册地址、方式与属地。累计型配额规则按它数，
--                       管理员「解除」只打 released_at，账号不受影响；删号后行仍保留（user_id 置空），
--                       否则「注册 → 注销 → 再注册」就能绕过累计上限
--   register_ip_lists   名单：放行 / 拒绝 / 放宽（配额乘倍数），单个地址或网段，可设到期
--   register_ip_events  拦截记录：拒绝、加验、观察、加验后放行、进入惩罚期、管理员解除
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

-- 自由文本转地址，转不了返回 NULL（回填与运维排查用）。带端口、方括号、掩码的写法都只取地址本身
CREATE OR REPLACE FUNCTION register_ip_try_inet(raw TEXT) RETURNS INET
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    value TEXT := btrim(COALESCE(raw, ''));
BEGIN
    IF value = '' OR length(value) > 64 THEN
        RETURN NULL;
    END IF;
    value := btrim(split_part(value, ',', 1));
    IF value ~ '^\[.*\](:[0-9]+)?$' THEN
        value := substring(value FROM '^\[(.*)\]');
    ELSIF value ~ '^[0-9.]+:[0-9]+$' THEN
        value := split_part(value, ':', 1);
    END IF;
    -- IPv4 映射的 IPv6 地址按 IPv4 存，与服务端规整口径一致
    IF value ~* '^::ffff:[0-9]+(\.[0-9]+){3}$' THEN
        value := substring(value FROM 8);
    END IF;
    RETURN host(value::inet)::inet;
EXCEPTION WHEN OTHERS THEN
    RETURN NULL;
END;
$$;

DO $$
DECLARE
    fresh BOOLEAN := to_regclass('register_ip_ledger') IS NULL;
BEGIN
    CREATE TABLE IF NOT EXISTS register_ip_ledger (
        id BIGSERIAL PRIMARY KEY,
        appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
        user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
        account VARCHAR(128) NOT NULL DEFAULT '',
        ip INET NOT NULL,
        method VARCHAR(16) NOT NULL DEFAULT '',
        country VARCHAR(64) NOT NULL DEFAULT '',
        province VARCHAR(64) NOT NULL DEFAULT '',
        city VARCHAR(64) NOT NULL DEFAULT '',
        isp VARCHAR(128) NOT NULL DEFAULT '',
        device_id VARCHAR(128) NOT NULL DEFAULT '',
        released_at TIMESTAMPTZ NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    );
    CREATE UNIQUE INDEX IF NOT EXISTS uq_register_ip_ledger_user
        ON register_ip_ledger (user_id) WHERE user_id IS NOT NULL;

    -- 只在第一次建表时从用户档案回填：历史账号的注册地址同样要算进累计上限，
    -- 否则开启「同一地址累计最多 N 个」的那一刻所有老地址都从零算起。
    -- 档案里的 register_ip 是自由文本，解析不了的跳过。
    IF fresh THEN
        INSERT INTO register_ip_ledger (appid, user_id, account, ip, method, province, city, isp, created_at)
        SELECT u.appid, u.id, u.account, parsed.ip,
               LEFT(COALESCE(p.extra->>'register_method', ''), 16),
               LEFT(COALESCE(p.register_province, ''), 64),
               LEFT(COALESCE(p.register_city, ''), 64),
               LEFT(COALESCE(p.register_isp, ''), 128),
               COALESCE(p.register_time, u.created_at)
        FROM users u
        JOIN user_profiles p ON p.user_id = u.id
        CROSS JOIN LATERAL (
            SELECT register_ip_try_inet(COALESCE(NULLIF(p.register_ip, ''), p.extra->>'register_ip')) AS ip
        ) parsed
        WHERE parsed.ip IS NOT NULL
        ON CONFLICT (user_id) WHERE user_id IS NOT NULL DO NOTHING;
    END IF;
END;
$$;

-- 累计规则按地址 / 网段数：网段的首末地址做范围扫描走这条索引
CREATE INDEX IF NOT EXISTS idx_register_ip_ledger_ip
    ON register_ip_ledger (appid, ip) WHERE released_at IS NULL;
-- 概览、台账列表、按窗口统计
CREATE INDEX IF NOT EXISTS idx_register_ip_ledger_created
    ON register_ip_ledger (appid, created_at DESC);
-- 累计型「同一城市」规则
CREATE INDEX IF NOT EXISTS idx_register_ip_ledger_city
    ON register_ip_ledger (appid, country, province, city) WHERE released_at IS NULL;

CREATE TABLE IF NOT EXISTS register_ip_lists (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    cidr CIDR NOT NULL,
    kind VARCHAR(16) NOT NULL,
    factor DOUBLE PRECISION NOT NULL DEFAULT 1,
    note VARCHAR(200) NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NULL,
    hits BIGINT NOT NULL DEFAULT 0,
    last_hit_at TIMESTAMPTZ NULL,
    created_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_register_ip_lists UNIQUE (appid, cidr)
);
ALTER TABLE register_ip_lists DROP CONSTRAINT IF EXISTS ck_register_ip_lists_kind;
ALTER TABLE register_ip_lists ADD CONSTRAINT ck_register_ip_lists_kind
    CHECK (kind IN ('allow', 'deny', 'elevated'));

CREATE TABLE IF NOT EXISTS register_ip_events (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    kind VARCHAR(24) NOT NULL,
    reason VARCHAR(16) NOT NULL DEFAULT '',
    rule_id VARCHAR(32) NOT NULL DEFAULT '',
    rule_name VARCHAR(64) NOT NULL DEFAULT '',
    method VARCHAR(16) NOT NULL DEFAULT '',
    ip VARCHAR(64) NOT NULL DEFAULT '',
    subnet VARCHAR(64) NOT NULL DEFAULT '',
    account VARCHAR(128) NOT NULL DEFAULT '',
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    country VARCHAR(64) NOT NULL DEFAULT '',
    province VARCHAR(64) NOT NULL DEFAULT '',
    city VARCHAR(64) NOT NULL DEFAULT '',
    isp VARCHAR(128) NOT NULL DEFAULT '',
    count_value INTEGER NOT NULL DEFAULT 0,
    limit_value INTEGER NOT NULL DEFAULT 0,
    detail VARCHAR(300) NOT NULL DEFAULT '',
    extra JSONB NOT NULL DEFAULT '{}'::jsonb,
    operator VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_register_ip_events_app
    ON register_ip_events (appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_register_ip_events_ip
    ON register_ip_events (appid, ip, created_at DESC);
