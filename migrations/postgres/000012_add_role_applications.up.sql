CREATE TABLE IF NOT EXISTS role_definitions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    role_key VARCHAR(64) NOT NULL,
    role_name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority INT NOT NULL DEFAULT 0,
    is_enabled BOOLEAN NOT NULL DEFAULT true,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (appid, role_key)
);

CREATE TABLE IF NOT EXISTS role_applications (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_role VARCHAR(64) NOT NULL,
    current_role_key VARCHAR(64) NOT NULL DEFAULT 'user',
    reason TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    priority VARCHAR(32) NOT NULL DEFAULT 'normal',
    valid_days INT NOT NULL DEFAULT 30,
    review_reason TEXT NOT NULL DEFAULT '',
    reviewed_by BIGINT,
    reviewed_by_name VARCHAR(128) NOT NULL DEFAULT '',
    reviewed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    device_info JSONB NOT NULL DEFAULT '{}'::jsonb,
    extra JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_role_definitions_app_enabled_priority ON role_definitions(appid, is_enabled, priority DESC);
CREATE INDEX IF NOT EXISTS idx_role_applications_app_status_created ON role_applications(appid, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_role_applications_user_created ON role_applications(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_role_applications_app_role_status ON role_applications(appid, requested_role, status, created_at DESC);
