-- 应用级第三方登录（OAuth2）渠道配置
-- 每个 App 独立维护自己的第三方登录渠道；未配置时回落到平台级 .env（OAUTH_*）。
CREATE TABLE IF NOT EXISTS app_oauth_providers (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- provider 即 oauth_bindings.provider 的取值，App 内唯一
    provider VARCHAR(32) NOT NULL,
    -- kind 决定使用哪个协议适配器（qq/wechat/weibo/github/generic）
    kind VARCHAR(32) NOT NULL DEFAULT 'generic',
    display_name VARCHAR(64) NOT NULL DEFAULT '',
    icon VARCHAR(64) NOT NULL DEFAULT '',
    color VARCHAR(32) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    client_id TEXT NOT NULL DEFAULT '',
    -- AES-GCM 密文，密钥派生自 SECURITY_MASTER_KEY，明文永不落库
    client_secret_cipher TEXT NOT NULL DEFAULT '',
    redirect_url TEXT NOT NULL DEFAULT '',
    auth_url TEXT NOT NULL DEFAULT '',
    token_url TEXT NOT NULL DEFAULT '',
    user_info_url TEXT NOT NULL DEFAULT '',
    scopes TEXT[] NOT NULL DEFAULT '{}',
    -- token 端点凭据传递方式：auto（先 params 失败再 basic）/ params / basic
    token_auth_style VARCHAR(16) NOT NULL DEFAULT 'auto',
    -- 用户信息端点凭据传递方式：header（Bearer）/ query（access_token=）
    user_info_auth_style VARCHAR(16) NOT NULL DEFAULT 'header',
    -- 自定义字段映射：{"id":"data.user_id","nickname":"data.name"}，支持点号路径
    profile_mapping JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- 附加授权参数：拼接到 authorize 链接（如 {"prompt":"consent"}）
    extra_auth_params JSONB NOT NULL DEFAULT '{}'::jsonb,
    allow_login BOOLEAN NOT NULL DEFAULT TRUE,
    allow_register BOOLEAN NOT NULL DEFAULT TRUE,
    allow_bind BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    remark VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_app_oauth_provider UNIQUE (appid, provider),
    CONSTRAINT ck_app_oauth_provider_slug CHECK (provider ~ '^[a-z0-9][a-z0-9_-]{1,31}$'),
    CONSTRAINT ck_app_oauth_provider_token_style CHECK (token_auth_style IN ('auto','params','basic')),
    CONSTRAINT ck_app_oauth_provider_userinfo_style CHECK (user_info_auth_style IN ('header','query')),
    CONSTRAINT ck_app_oauth_provider_scopes CHECK (COALESCE(array_length(scopes, 1), 0) <= 32),
    CONSTRAINT ck_app_oauth_provider_mapping CHECK (jsonb_typeof(profile_mapping) = 'object'),
    CONSTRAINT ck_app_oauth_provider_auth_params CHECK (jsonb_typeof(extra_auth_params) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_app_oauth_providers_enabled
ON app_oauth_providers(appid, enabled, sort_order, id);
