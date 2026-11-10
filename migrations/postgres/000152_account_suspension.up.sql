-- 账号封禁：应用级 / 全站、对外原因、申诉复审（docs/account-suspension.md）

-- ── 账号封禁扩展 ──
ALTER TABLE user_account_bans ADD COLUMN IF NOT EXISTS level VARCHAR(16) NOT NULL DEFAULT 'app';
ALTER TABLE user_account_bans ADD COLUMN IF NOT EXISTS platform_ban_id BIGINT NULL;
ALTER TABLE user_account_bans ADD COLUMN IF NOT EXISTS reason_code VARCHAR(32) NOT NULL DEFAULT 'policy';
ALTER TABLE user_account_bans ADD COLUMN IF NOT EXISTS public_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE user_account_bans ADD COLUMN IF NOT EXISTS appealable BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE user_account_bans ADD COLUMN IF NOT EXISTS source VARCHAR(16) NOT NULL DEFAULT 'manual';
DO $$ BEGIN
    ALTER TABLE user_account_bans ADD CONSTRAINT chk_user_account_bans_level CHECK (level IN ('app', 'platform'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
-- 老数据里风控下的封禁按记录的来源补上
UPDATE user_account_bans SET source = 'risk', reason_code = 'fraud'
WHERE source = 'manual' AND (evidence->>'source' = 'risk' OR banned_by_admin_name LIKE '风控平台%');

-- ── 全站封禁：封的是「这个人」，主体是邮箱、手机号、第三方身份、设备的摘要 ──
CREATE TABLE IF NOT EXISTS platform_bans (
    id BIGSERIAL PRIMARY KEY,
    ban_type VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    reason TEXT NOT NULL DEFAULT '',
    public_reason TEXT NOT NULL DEFAULT '',
    reason_code VARCHAR(32) NOT NULL DEFAULT 'policy',
    source VARCHAR(16) NOT NULL DEFAULT 'manual',
    appealable BOOLEAN NOT NULL DEFAULT TRUE,
    origin_app_id BIGINT NULL,
    origin_user_id BIGINT NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_admin_id BIGINT NULL,
    created_by_name VARCHAR(255) NOT NULL DEFAULT '',
    revoked_by_admin_id BIGINT NULL,
    revoked_by_name VARCHAR(255) NOT NULL DEFAULT '',
    revoke_reason TEXT NOT NULL DEFAULT '',
    start_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    end_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_platform_bans_type CHECK (ban_type IN ('temporary', 'permanent')),
    CONSTRAINT chk_platform_bans_status CHECK (status IN ('active', 'expired', 'revoked')),
    CONSTRAINT chk_platform_bans_end CHECK ((ban_type = 'permanent' AND end_at IS NULL) OR (ban_type = 'temporary' AND end_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS idx_platform_bans_status ON platform_bans (status, created_at DESC);

CREATE TABLE IF NOT EXISTS platform_ban_subjects (
    id BIGSERIAL PRIMARY KEY,
    platform_ban_id BIGINT NOT NULL REFERENCES platform_bans(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,          -- email / phone / identity / device
    digest CHAR(64) NOT NULL,           -- HMAC-SHA256（主密钥）的十六进制
    masked VARCHAR(160) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_platform_ban_subject UNIQUE (platform_ban_id, kind, digest),
    CONSTRAINT chk_platform_ban_subject_kind CHECK (kind IN ('email', 'phone', 'identity', 'device'))
);
CREATE INDEX IF NOT EXISTS idx_platform_ban_subjects_digest ON platform_ban_subjects (kind, digest);

CREATE INDEX IF NOT EXISTS idx_user_account_bans_platform ON user_account_bans (platform_ban_id) WHERE platform_ban_id IS NOT NULL;

-- ── 申诉 ──
CREATE TABLE IF NOT EXISTS ban_appeals (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ban_id BIGINT NULL REFERENCES user_account_bans(id) ON DELETE SET NULL,
    platform_ban_id BIGINT NULL REFERENCES platform_bans(id) ON DELETE SET NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    content TEXT NOT NULL,
    contact_email VARCHAR(256) NOT NULL DEFAULT '',
    locale VARCHAR(16) NOT NULL DEFAULT 'zh-CN',
    source VARCHAR(16) NOT NULL DEFAULT 'app',
    client_ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent VARCHAR(512) NOT NULL DEFAULT '',
    -- 决策模型
    model_verdict VARCHAR(16) NOT NULL DEFAULT '',
    model_confidence REAL NOT NULL DEFAULT 0,
    model_summary TEXT NOT NULL DEFAULT '',
    model_reply TEXT NOT NULL DEFAULT '',
    model_name VARCHAR(128) NOT NULL DEFAULT '',
    model_error TEXT NOT NULL DEFAULT '',
    risk_level VARCHAR(16) NOT NULL DEFAULT '',
    risk_evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    model_reviewed_at TIMESTAMPTZ NULL,
    review_attempts INT NOT NULL DEFAULT 0,
    -- 结论
    decided_by VARCHAR(16) NOT NULL DEFAULT '',
    reviewer_admin_id BIGINT NULL,
    reviewer_name VARCHAR(255) NOT NULL DEFAULT '',
    review_note TEXT NOT NULL DEFAULT '',
    reply TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at TIMESTAMPTZ NULL,
    CONSTRAINT chk_ban_appeals_status CHECK (status IN ('pending', 'reviewing', 'approved', 'rejected', 'withdrawn'))
);
CREATE INDEX IF NOT EXISTS idx_ban_appeals_app_status ON ban_appeals (appid, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ban_appeals_user ON ban_appeals (appid, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ban_appeals_ban ON ban_appeals (ban_id);
-- 一条封禁同时只能有一条进行中的申诉
CREATE UNIQUE INDEX IF NOT EXISTS uq_ban_appeals_open ON ban_appeals (ban_id) WHERE status IN ('pending', 'reviewing') AND ban_id IS NOT NULL;
-- 待模型复审的（兜底认领）
CREATE INDEX IF NOT EXISTS idx_ban_appeals_unreviewed ON ban_appeals (created_at) WHERE status = 'pending' AND model_reviewed_at IS NULL;

-- 应用申诉设置；appid = 0 是平台默认
CREATE TABLE IF NOT EXISTS ban_appeal_settings (
    appid BIGINT PRIMARY KEY,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by VARCHAR(255) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
