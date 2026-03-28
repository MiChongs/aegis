-- 管理员会话持久化记录
CREATE TABLE IF NOT EXISTS admin_sessions (
    id VARCHAR(64) PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    device VARCHAR(128) NOT NULL DEFAULT '',
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_revoked BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_by BIGINT NULL,
    revoked_at TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_admin ON admin_sessions(admin_id, is_revoked);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_expires ON admin_sessions(expires_at) WHERE NOT is_revoked;

-- 临时权限授予
CREATE TABLE IF NOT EXISTS admin_temp_permissions (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    permission VARCHAR(128) NOT NULL,
    app_id BIGINT NULL,
    granted_by BIGINT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    is_revoked BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_admin_temp_perms_active ON admin_temp_permissions(admin_id) WHERE NOT is_revoked;

-- 全局代理授权
CREATE TABLE IF NOT EXISTS admin_delegations (
    id BIGSERIAL PRIMARY KEY,
    delegator_id BIGINT NOT NULL REFERENCES admin_accounts(id),
    delegate_id BIGINT NOT NULL REFERENCES admin_accounts(id),
    scope VARCHAR(32) NOT NULL DEFAULT 'all',
    scope_id BIGINT NULL,
    granted_by BIGINT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    is_revoked BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_admin_delegations_delegate ON admin_delegations(delegate_id) WHERE NOT is_revoked;
