CREATE TABLE IF NOT EXISTS sites (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    header TEXT NOT NULL DEFAULT '',
    name VARCHAR(255) NOT NULL,
    url TEXT NOT NULL,
    type VARCHAR(64) NOT NULL DEFAULT 'other',
    description TEXT NOT NULL DEFAULT '',
    category VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    audit_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    audit_reason TEXT NOT NULL DEFAULT '',
    audit_admin_id BIGINT,
    audit_at TIMESTAMPTZ,
    is_pinned BOOLEAN NOT NULL DEFAULT false,
    view_count BIGINT NOT NULL DEFAULT 0,
    like_count BIGINT NOT NULL DEFAULT 0,
    extra JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS site_audits (
    id BIGSERIAL PRIMARY KEY,
    site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status VARCHAR(32) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    admin_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sites_app_status_created ON sites(appid, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sites_app_audit_status_created ON sites(appid, audit_status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sites_user_created ON sites(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sites_app_pinned_created ON sites(appid, is_pinned DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_audits_site_created ON site_audits(site_id, created_at DESC);
