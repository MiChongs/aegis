-- IP 封禁记录表
CREATE TABLE IF NOT EXISTS ip_bans (
    id            BIGSERIAL PRIMARY KEY,
    ip            TEXT NOT NULL,
    reason        TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL DEFAULT 'manual',
    trigger_rule  TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL DEFAULT 'medium',
    duration      BIGINT NOT NULL DEFAULT 0,
    expires_at    TIMESTAMPTZ,
    status        TEXT NOT NULL DEFAULT 'active',
    revoked_by    BIGINT,
    revoked_at    TIMESTAMPTZ,
    country       TEXT NOT NULL DEFAULT '',
    country_code  TEXT NOT NULL DEFAULT '',
    region        TEXT NOT NULL DEFAULT '',
    city          TEXT NOT NULL DEFAULT '',
    isp           TEXT NOT NULL DEFAULT '',
    trigger_count INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ip_bans_active_ip   ON ip_bans (ip) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_ip_bans_status             ON ip_bans (status);
CREATE INDEX IF NOT EXISTS idx_ip_bans_created_at         ON ip_bans (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ip_bans_expires_at         ON ip_bans (expires_at) WHERE expires_at IS NOT NULL AND status = 'active';
