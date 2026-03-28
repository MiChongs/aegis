-- 存储对象索引表
CREATE TABLE IF NOT EXISTS storage_objects (
    id BIGSERIAL PRIMARY KEY,
    config_id BIGINT NOT NULL REFERENCES storage_configs(id) ON DELETE CASCADE,
    app_id BIGINT NULL,
    object_key VARCHAR(1024) NOT NULL,
    file_name VARCHAR(512) NOT NULL DEFAULT '',
    content_type VARCHAR(128) NOT NULL DEFAULT '',
    size BIGINT NOT NULL DEFAULT 0,
    etag VARCHAR(128) NOT NULL DEFAULT '',
    uploaded_by BIGINT NULL,
    uploader_type VARCHAR(16) NOT NULL DEFAULT 'user',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_storage_objects_config ON storage_objects(config_id, status);
CREATE INDEX IF NOT EXISTS idx_storage_objects_app ON storage_objects(app_id, status);

-- 存储规则
CREATE TABLE IF NOT EXISTS storage_rules (
    id BIGSERIAL PRIMARY KEY,
    config_id BIGINT NULL REFERENCES storage_configs(id) ON DELETE CASCADE,
    app_id BIGINT NULL,
    name VARCHAR(128) NOT NULL,
    rule_type VARCHAR(32) NOT NULL,
    rule_data JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- CDN 与防盗链配置
CREATE TABLE IF NOT EXISTS storage_cdn_configs (
    id BIGSERIAL PRIMARY KEY,
    config_id BIGINT NOT NULL REFERENCES storage_configs(id) ON DELETE CASCADE,
    cdn_domain VARCHAR(512) NOT NULL DEFAULT '',
    cdn_protocol VARCHAR(8) NOT NULL DEFAULT 'https',
    cache_max_age INT NOT NULL DEFAULT 86400,
    referer_whitelist TEXT[] NOT NULL DEFAULT '{}',
    referer_blacklist TEXT[] NOT NULL DEFAULT '{}',
    ip_whitelist TEXT[] NOT NULL DEFAULT '{}',
    sign_url_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    sign_url_secret VARCHAR(256) NOT NULL DEFAULT '',
    sign_url_ttl INT NOT NULL DEFAULT 3600,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 图片处理规则
CREATE TABLE IF NOT EXISTS storage_image_rules (
    id BIGSERIAL PRIMARY KEY,
    config_id BIGINT NULL REFERENCES storage_configs(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    rule_type VARCHAR(32) NOT NULL,
    rule_data JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 存储用量快照
CREATE TABLE IF NOT EXISTS storage_usage_snapshots (
    id BIGSERIAL PRIMARY KEY,
    config_id BIGINT NOT NULL REFERENCES storage_configs(id) ON DELETE CASCADE,
    app_id BIGINT NULL,
    total_files BIGINT NOT NULL DEFAULT 0,
    total_size BIGINT NOT NULL DEFAULT 0,
    active_files BIGINT NOT NULL DEFAULT 0,
    deleted_files BIGINT NOT NULL DEFAULT 0,
    snapshot_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_storage_usage_config ON storage_usage_snapshots(config_id, snapshot_at DESC);
