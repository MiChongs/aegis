-- +migrate Up
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    account VARCHAR(128) NOT NULL,
    password_hash TEXT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    disabled_end_time TIMESTAMPTZ NULL,
    vip_expire_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_users_appid_account UNIQUE (appid, account)
);

CREATE TABLE IF NOT EXISTS user_profiles (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    nickname VARCHAR(128) NULL,
    avatar TEXT NULL,
    email VARCHAR(255) NULL,
    extra JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_settings (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category VARCHAR(64) NOT NULL,
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_settings_user_category UNIQUE (user_id, category)
);

CREATE TABLE IF NOT EXISTS oauth_bindings (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL,
    provider_user_id VARCHAR(191) NOT NULL,
    union_id VARCHAR(191) NULL,
    access_token TEXT NULL,
    refresh_token TEXT NULL,
    raw_profile JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_oauth_binding_provider_user UNIQUE (appid, provider, provider_user_id)
);

CREATE TABLE IF NOT EXISTS login_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    appid BIGINT NOT NULL,
    login_type VARCHAR(32) NOT NULL,
    provider VARCHAR(32) NULL,
    token_jti VARCHAR(128) NULL,
    login_ip INET NULL,
    device_id VARCHAR(191) NULL,
    user_agent TEXT NULL,
    status VARCHAR(32) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS session_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    appid BIGINT NOT NULL,
    token_jti VARCHAR(128) NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_appid_enabled ON users(appid, enabled);
CREATE INDEX IF NOT EXISTS idx_users_disabled_end_time ON users(disabled_end_time);
CREATE INDEX IF NOT EXISTS idx_oauth_bindings_user_id ON oauth_bindings(user_id);
CREATE INDEX IF NOT EXISTS idx_login_audit_user_time ON login_audit_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_login_audit_token_jti ON login_audit_logs(token_jti);
CREATE INDEX IF NOT EXISTS idx_session_audit_user_time ON session_audit_logs(user_id, created_at DESC);
