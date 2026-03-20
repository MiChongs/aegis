CREATE TABLE IF NOT EXISTS storage_configs (
    id BIGSERIAL PRIMARY KEY,
    scope VARCHAR(16) NOT NULL CHECK (scope IN ('global', 'app')),
    appid BIGINT NULL REFERENCES apps(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL,
    config_name VARCHAR(64) NOT NULL,
    access_mode VARCHAR(16) NOT NULL DEFAULT 'public' CHECK (access_mode IN ('public', 'private')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    proxy_download BOOLEAN NOT NULL DEFAULT TRUE,
    base_url TEXT NULL,
    root_path TEXT NULL DEFAULT '',
    description TEXT NULL,
    config_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_storage_scope_appid CHECK (
        (scope = 'global' AND appid IS NULL) OR
        (scope = 'app' AND appid IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_storage_configs_scope_appid_name
    ON storage_configs (scope, COALESCE(appid, 0), config_name);

CREATE INDEX IF NOT EXISTS idx_storage_configs_scope_appid
    ON storage_configs (scope, appid);

CREATE INDEX IF NOT EXISTS idx_storage_configs_scope_enabled_default
    ON storage_configs (scope, appid, enabled, is_default);
