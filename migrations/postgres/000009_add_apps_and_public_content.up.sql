-- +migrate Up
CREATE TABLE IF NOT EXISTS apps (
    id BIGINT PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    app_key VARCHAR(191) NULL,
    status BOOLEAN NOT NULL DEFAULT TRUE,
    disabled_reason VARCHAR(255) NULL,
    register_status BOOLEAN NOT NULL DEFAULT TRUE,
    disabled_register_reason VARCHAR(255) NULL,
    login_status BOOLEAN NOT NULL DEFAULT TRUE,
    disabled_login_reason VARCHAR(255) NULL,
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS banners (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    header TEXT NULL,
    title TEXT NOT NULL,
    content TEXT NULL,
    url TEXT NULL,
    type VARCHAR(32) NOT NULL DEFAULT 'url',
    position INTEGER NOT NULL DEFAULT 0,
    status BOOLEAN NOT NULL DEFAULT TRUE,
    start_time TIMESTAMPTZ NULL,
    end_time TIMESTAMPTZ NULL,
    click_count BIGINT NOT NULL DEFAULT 0,
    view_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notices (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    title TEXT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_apps_status ON apps(status);
CREATE INDEX IF NOT EXISTS idx_banners_app_position ON banners(appid, position ASC);
CREATE INDEX IF NOT EXISTS idx_banners_app_status_time ON banners(appid, status, start_time, end_time);
CREATE INDEX IF NOT EXISTS idx_notices_app_created ON notices(appid, created_at DESC);

INSERT INTO apps (id, name, status, register_status, login_status, settings, created_at, updated_at)
VALUES (10000, '默认应用', TRUE, TRUE, TRUE, '{}'::jsonb, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;
