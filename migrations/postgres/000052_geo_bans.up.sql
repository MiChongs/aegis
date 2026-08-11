-- 地域/ASN 封禁规则表（平台级全局）
-- 检查顺序：IP 精确封禁 → 地域规则；即 IP 优先

CREATE TABLE IF NOT EXISTS geo_bans (
    id            BIGSERIAL PRIMARY KEY,
    scope_type    VARCHAR(16) NOT NULL,       -- country | region | city | asn | isp
    scope_value   VARCHAR(128) NOT NULL,      -- 如 CN / US-CA / AS15169
    mode          VARCHAR(32) NOT NULL DEFAULT '',
    reason        VARCHAR(255) NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    created_by    BIGINT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ NULL,
    match_count   BIGINT NOT NULL DEFAULT 0,
    last_match_at TIMESTAMPTZ NULL,
    CONSTRAINT geo_bans_scope_unique UNIQUE (scope_type, scope_value)
);

CREATE INDEX IF NOT EXISTS idx_geo_bans_enabled ON geo_bans(enabled) WHERE enabled = TRUE;
CREATE INDEX IF NOT EXISTS idx_geo_bans_scope_type ON geo_bans(scope_type);
CREATE INDEX IF NOT EXISTS idx_geo_bans_created_at ON geo_bans(created_at DESC);

COMMENT ON TABLE geo_bans IS '地域 / ASN / ISP 封禁规则（按命中封禁国家、地区、AS 等）';
COMMENT ON COLUMN geo_bans.scope_type IS 'country | region | city | asn | isp';
COMMENT ON COLUMN geo_bans.scope_value IS '作用域值。country 用 ISO 3166-1 alpha-2 (CN/US/RU)；region 建议 国-省(CN-BJ)；asn 格式 AS15169';
COMMENT ON COLUMN geo_bans.mode IS '响应模式；空字符串 = 使用平台默认';
