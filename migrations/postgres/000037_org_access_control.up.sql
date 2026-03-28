-- 管理范围：管理员→组织绑定
ALTER TABLE admin_assignments ADD COLUMN IF NOT EXISTS org_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL;

-- 审批链配置
CREATE TABLE IF NOT EXISTS approval_chains (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    trigger_type VARCHAR(64) NOT NULL,
    steps JSONB NOT NULL DEFAULT '[]',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 审批实例
CREATE TABLE IF NOT EXISTS approval_instances (
    id BIGSERIAL PRIMARY KEY,
    chain_id BIGINT NOT NULL REFERENCES approval_chains(id) ON DELETE CASCADE,
    org_id BIGINT NOT NULL,
    trigger_type VARCHAR(64) NOT NULL,
    requester_id BIGINT NOT NULL,
    subject_data JSONB NOT NULL DEFAULT '{}',
    current_step INT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    steps_result JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT approval_instances_status_chk CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled'))
);
CREATE INDEX IF NOT EXISTS idx_approval_instances_org_status ON approval_instances(org_id, status);
CREATE INDEX IF NOT EXISTS idx_approval_instances_requester ON approval_instances(requester_id);

-- 组织级权限模板
CREATE TABLE IF NOT EXISTS org_permission_templates (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    permissions TEXT[] NOT NULL DEFAULT '{}',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 组织→应用资源绑定
CREATE TABLE IF NOT EXISTS org_app_bindings (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    app_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, app_id)
);

-- 跨部门协作组
CREATE TABLE IF NOT EXISTS collaboration_groups (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    dept_ids BIGINT[] NOT NULL DEFAULT '{}',
    permissions TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
