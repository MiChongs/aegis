CREATE TABLE IF NOT EXISTS admin_accounts (
    id BIGSERIAL PRIMARY KEY,
    account VARCHAR(64) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name VARCHAR(128) NOT NULL DEFAULT '',
    email VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    is_super_admin BOOLEAN NOT NULL DEFAULT FALSE,
    last_login_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT admin_accounts_status_chk CHECK (status IN ('active', 'disabled'))
);

CREATE TABLE IF NOT EXISTS admin_assignments (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    role_key VARCHAR(64) NOT NULL,
    appid BIGINT NULL REFERENCES apps(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT admin_assignments_role_chk CHECK (role_key IN ('platform_admin', 'app_admin', 'app_operator', 'app_auditor', 'app_viewer'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_admin_assignments_unique
    ON admin_assignments (admin_id, role_key, COALESCE(appid, 0));

CREATE INDEX IF NOT EXISTS idx_admin_assignments_admin_id ON admin_assignments(admin_id);
CREATE INDEX IF NOT EXISTS idx_admin_assignments_appid ON admin_assignments(appid);
CREATE INDEX IF NOT EXISTS idx_admin_accounts_status ON admin_accounts(status);
