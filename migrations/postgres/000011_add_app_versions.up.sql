CREATE TABLE IF NOT EXISTS app_version_channels (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    code VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT false,
    status BOOLEAN NOT NULL DEFAULT true,
    target_audience JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (appid, code)
);

CREATE TABLE IF NOT EXISTS app_versions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    channel_id BIGINT REFERENCES app_version_channels(id) ON DELETE SET NULL,
    version VARCHAR(64) NOT NULL,
    version_code BIGINT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    release_notes TEXT NOT NULL DEFAULT '',
    download_url TEXT NOT NULL DEFAULT '',
    file_size BIGINT NOT NULL DEFAULT 0,
    file_hash VARCHAR(255) NOT NULL DEFAULT '',
    force_update BOOLEAN NOT NULL DEFAULT false,
    update_type VARCHAR(32) NOT NULL DEFAULT 'optional',
    platform VARCHAR(32) NOT NULL DEFAULT 'all',
    min_os_version VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'published',
    download_count BIGINT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS app_version_channel_users (
    channel_id BIGINT NOT NULL REFERENCES app_version_channels(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_app_versions_app_platform_code ON app_versions(appid, platform, version_code DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_versions_channel_status_code ON app_versions(channel_id, status, version_code DESC);
CREATE INDEX IF NOT EXISTS idx_app_version_channels_app_default ON app_version_channels(appid, is_default DESC, status);
CREATE INDEX IF NOT EXISTS idx_app_version_channel_users_user ON app_version_channel_users(user_id, appid);
