-- 全局统一身份表
CREATE TABLE IF NOT EXISTS global_identities (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) NULL,
    phone VARCHAR(64) NULL,
    display_name VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    risk_score INT NOT NULL DEFAULT 0,
    risk_level VARCHAR(16) NOT NULL DEFAULT 'normal',
    lifecycle_state VARCHAR(32) NOT NULL DEFAULT 'registered',
    lifecycle_changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_global_identities_email ON global_identities(email) WHERE email IS NOT NULL AND status != 'deleted';
CREATE UNIQUE INDEX IF NOT EXISTS idx_global_identities_phone ON global_identities(phone) WHERE phone IS NOT NULL AND status != 'deleted';
CREATE INDEX IF NOT EXISTS idx_global_identities_status ON global_identities(status, lifecycle_state);
CREATE INDEX IF NOT EXISTS idx_global_identities_risk ON global_identities(risk_level) WHERE risk_level != 'normal';

-- 跨应用用户映射
CREATE TABLE IF NOT EXISTS identity_user_mappings (
    id BIGSERIAL PRIMARY KEY,
    identity_id BIGINT NOT NULL REFERENCES global_identities(id) ON DELETE CASCADE,
    app_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(app_id, user_id),
    UNIQUE(identity_id, app_id)
);

-- 用户标签定义
CREATE TABLE IF NOT EXISTS user_tags (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE,
    color VARCHAR(16) NOT NULL DEFAULT '#6366f1',
    description TEXT NOT NULL DEFAULT '',
    created_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 用户标签关联
CREATE TABLE IF NOT EXISTS user_tag_assignments (
    id BIGSERIAL PRIMARY KEY,
    identity_id BIGINT NOT NULL REFERENCES global_identities(id) ON DELETE CASCADE,
    tag_id BIGINT NOT NULL REFERENCES user_tags(id) ON DELETE CASCADE,
    assigned_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(identity_id, tag_id)
);

-- 用户分群
CREATE TABLE IF NOT EXISTS user_segments (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    segment_type VARCHAR(16) NOT NULL DEFAULT 'static',
    rules JSONB NOT NULL DEFAULT '{}',
    member_count INT NOT NULL DEFAULT 0,
    created_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 静态分群成员
CREATE TABLE IF NOT EXISTS user_segment_members (
    id BIGSERIAL PRIMARY KEY,
    segment_id BIGINT NOT NULL REFERENCES user_segments(id) ON DELETE CASCADE,
    identity_id BIGINT NOT NULL REFERENCES global_identities(id) ON DELETE CASCADE,
    added_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(segment_id, identity_id)
);

-- 黑白名单
CREATE TABLE IF NOT EXISTS user_lists (
    id BIGSERIAL PRIMARY KEY,
    list_type VARCHAR(16) NOT NULL,
    identity_id BIGINT NULL REFERENCES global_identities(id) ON DELETE CASCADE,
    email VARCHAR(255) NULL,
    phone VARCHAR(64) NULL,
    ip VARCHAR(64) NULL,
    reason TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NULL,
    created_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_user_lists_type ON user_lists(list_type);

-- 账号合并记录
CREATE TABLE IF NOT EXISTS identity_merges (
    id BIGSERIAL PRIMARY KEY,
    primary_id BIGINT NOT NULL REFERENCES global_identities(id),
    merged_id BIGINT NOT NULL REFERENCES global_identities(id),
    merged_by BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'completed',
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 用户申诉
CREATE TABLE IF NOT EXISTS user_appeals (
    id BIGSERIAL PRIMARY KEY,
    identity_id BIGINT NOT NULL REFERENCES global_identities(id),
    appeal_type VARCHAR(32) NOT NULL,
    reason TEXT NOT NULL,
    evidence TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    reviewer_id BIGINT NULL,
    review_comment TEXT NOT NULL DEFAULT '',
    reviewed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_user_appeals_status ON user_appeals(status);

-- 注销请求
CREATE TABLE IF NOT EXISTS deactivation_requests (
    id BIGSERIAL PRIMARY KEY,
    identity_id BIGINT NOT NULL REFERENCES global_identities(id),
    reason TEXT NOT NULL DEFAULT '',
    cooling_days INT NOT NULL DEFAULT 14,
    scheduled_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
