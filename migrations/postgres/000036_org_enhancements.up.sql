-- 岗位表
CREATE TABLE IF NOT EXISTS positions (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    code VARCHAR(64) NOT NULL,
    level INT NOT NULL DEFAULT 0,
    description TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, code)
);

-- 部门成员增强：岗位、汇报线、代理人
ALTER TABLE department_members
    ADD COLUMN IF NOT EXISTS position_id BIGINT REFERENCES positions(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS job_title VARCHAR(128) DEFAULT '',
    ADD COLUMN IF NOT EXISTS reporting_to BIGINT REFERENCES admin_accounts(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS delegate_to BIGINT REFERENCES admin_accounts(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS delegate_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_dept_members_reporting ON department_members(reporting_to);
CREATE INDEX IF NOT EXISTS idx_positions_org ON positions(org_id);
