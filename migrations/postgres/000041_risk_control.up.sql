-- 风险规则定义
CREATE TABLE IF NOT EXISTS risk_rules (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    scene VARCHAR(32) NOT NULL,
    condition_type VARCHAR(32) NOT NULL,
    condition_data JSONB NOT NULL DEFAULT '{}',
    score INT NOT NULL DEFAULT 10,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    priority INT NOT NULL DEFAULT 100,
    created_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_rules_scene ON risk_rules(scene, is_active);

-- 风险评估记录
CREATE TABLE IF NOT EXISTS risk_assessments (
    id BIGSERIAL PRIMARY KEY,
    scene VARCHAR(32) NOT NULL,
    app_id BIGINT NULL,
    user_id BIGINT NULL,
    identity_id BIGINT NULL,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    device_id VARCHAR(256) NOT NULL DEFAULT '',
    total_score INT NOT NULL DEFAULT 0,
    risk_level VARCHAR(16) NOT NULL DEFAULT 'normal',
    matched_rules JSONB NOT NULL DEFAULT '[]',
    action VARCHAR(32) NOT NULL DEFAULT 'pass',
    action_detail TEXT NOT NULL DEFAULT '',
    reviewed BOOLEAN NOT NULL DEFAULT FALSE,
    reviewer_id BIGINT NULL,
    review_result VARCHAR(16) NULL,
    review_comment TEXT NOT NULL DEFAULT '',
    reviewed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_assessments_scene ON risk_assessments(scene, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_risk_assessments_review ON risk_assessments(action) WHERE action = 'review' AND NOT reviewed;

-- 设备指纹库
CREATE TABLE IF NOT EXISTS device_fingerprints (
    id BIGSERIAL PRIMARY KEY,
    device_id VARCHAR(256) NOT NULL UNIQUE,
    user_id BIGINT NULL,
    app_id BIGINT NULL,
    fingerprint JSONB NOT NULL DEFAULT '{}',
    risk_tag VARCHAR(32) NOT NULL DEFAULT 'normal',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    seen_count INT NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_device_fingerprints_risk ON device_fingerprints(risk_tag) WHERE risk_tag != 'normal';

-- IP 风险库
CREATE TABLE IF NOT EXISTS ip_risk_records (
    id BIGSERIAL PRIMARY KEY,
    ip VARCHAR(64) NOT NULL UNIQUE,
    risk_tag VARCHAR(32) NOT NULL DEFAULT 'normal',
    risk_score INT NOT NULL DEFAULT 0,
    country VARCHAR(64) NOT NULL DEFAULT '',
    region VARCHAR(128) NOT NULL DEFAULT '',
    isp VARCHAR(128) NOT NULL DEFAULT '',
    is_proxy BOOLEAN NOT NULL DEFAULT FALSE,
    is_vpn BOOLEAN NOT NULL DEFAULT FALSE,
    is_tor BOOLEAN NOT NULL DEFAULT FALSE,
    is_datacenter BOOLEAN NOT NULL DEFAULT FALSE,
    total_requests BIGINT NOT NULL DEFAULT 0,
    total_blocks BIGINT NOT NULL DEFAULT 0,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_ip_risk_tag ON ip_risk_records(risk_tag) WHERE risk_tag != 'normal';

-- 自动处置策略
CREATE TABLE IF NOT EXISTS risk_actions (
    id BIGSERIAL PRIMARY KEY,
    scene VARCHAR(32) NOT NULL,
    min_score INT NOT NULL,
    max_score INT NULL,
    action VARCHAR(32) NOT NULL,
    ban_duration INT NOT NULL DEFAULT 0,
    description TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_actions_scene ON risk_actions(scene, is_active);
