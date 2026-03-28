CREATE TABLE IF NOT EXISTS admin_totp_secrets (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    secret_ciphertext TEXT NOT NULL,
    issuer TEXT NOT NULL,
    account_name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    enabled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_verified_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_admin_totp_secrets_admin UNIQUE (admin_id)
);

CREATE INDEX IF NOT EXISTS idx_admin_totp_secrets_admin ON admin_totp_secrets(admin_id);

CREATE TABLE IF NOT EXISTS admin_recovery_codes (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    code_hash CHAR(64) NOT NULL,
    code_hint VARCHAR(32) NOT NULL,
    used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_admin_recovery_codes_admin_hash UNIQUE (admin_id, code_hash)
);

CREATE INDEX IF NOT EXISTS idx_admin_recovery_codes_admin ON admin_recovery_codes(admin_id);
CREATE INDEX IF NOT EXISTS idx_admin_recovery_codes_unused ON admin_recovery_codes(admin_id, used_at);

CREATE TABLE IF NOT EXISTS admin_passkeys (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL,
    credential_name VARCHAR(128) NULL,
    credential_json JSONB NOT NULL,
    aaguid BYTEA NULL,
    sign_count BIGINT NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_admin_passkeys_credential UNIQUE (credential_id)
);

CREATE INDEX IF NOT EXISTS idx_admin_passkeys_admin ON admin_passkeys(admin_id);
CREATE INDEX IF NOT EXISTS idx_admin_passkeys_created_at ON admin_passkeys(admin_id, created_at DESC);
