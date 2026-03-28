CREATE TABLE IF NOT EXISTS user_account_bans (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ban_type VARCHAR(16) NOT NULL,
    ban_scope VARCHAR(16) NOT NULL DEFAULT 'login',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    reason TEXT NOT NULL DEFAULT '',
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    banned_by_admin_id BIGINT NULL,
    banned_by_admin_name VARCHAR(255) NOT NULL DEFAULT '',
    revoked_by_admin_id BIGINT NULL,
    revoked_by_admin_name VARCHAR(255) NOT NULL DEFAULT '',
    revoke_reason TEXT NOT NULL DEFAULT '',
    start_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    end_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_user_account_bans_type CHECK (ban_type IN ('temporary', 'permanent')),
    CONSTRAINT chk_user_account_bans_scope CHECK (ban_scope IN ('login', 'all')),
    CONSTRAINT chk_user_account_bans_status CHECK (status IN ('active', 'expired', 'revoked')),
    CONSTRAINT chk_user_account_bans_end_at CHECK (
        (ban_type = 'permanent' AND end_at IS NULL) OR
        (ban_type = 'temporary' AND end_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_account_bans_active_user
    ON user_account_bans (appid, user_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_user_account_bans_user_status_created
    ON user_account_bans (appid, user_id, status, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_user_account_bans_status_end_at
    ON user_account_bans (status, end_at)
    WHERE status = 'active' AND end_at IS NOT NULL;
