CREATE TABLE IF NOT EXISTS user_totp_secrets (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    secret_ciphertext TEXT NOT NULL,
    issuer TEXT NOT NULL,
    account_name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    enabled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_verified_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_totp_secrets_user UNIQUE (user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_totp_secrets_appid_user ON user_totp_secrets(appid, user_id);

CREATE TABLE IF NOT EXISTS user_recovery_codes (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash CHAR(64) NOT NULL,
    code_hint VARCHAR(32) NOT NULL,
    used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_recovery_codes_user_hash UNIQUE (user_id, code_hash)
);

CREATE INDEX IF NOT EXISTS idx_user_recovery_codes_appid_user ON user_recovery_codes(appid, user_id);
CREATE INDEX IF NOT EXISTS idx_user_recovery_codes_user_unused ON user_recovery_codes(user_id, used_at);

CREATE TABLE IF NOT EXISTS user_passkeys (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL,
    credential_name VARCHAR(128) NULL,
    credential_json JSONB NOT NULL,
    aaguid BYTEA NULL,
    sign_count BIGINT NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_passkeys_appid_credential UNIQUE (appid, credential_id)
);

CREATE INDEX IF NOT EXISTS idx_user_passkeys_appid_user ON user_passkeys(appid, user_id);
CREATE INDEX IF NOT EXISTS idx_user_passkeys_user_created_at ON user_passkeys(user_id, created_at DESC);
