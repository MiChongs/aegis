-- 组织表
CREATE TABLE IF NOT EXISTS organizations (
    id          BIGSERIAL    PRIMARY KEY,
    name        VARCHAR(128) NOT NULL,
    code        VARCHAR(64)  NOT NULL UNIQUE,
    description TEXT         NOT NULL DEFAULT '',
    logo_url    TEXT         NOT NULL DEFAULT '',
    status      VARCHAR(16)  NOT NULL DEFAULT 'active',
    created_by  BIGINT       NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT organizations_status_chk CHECK (status IN ('active', 'disabled'))
);

-- 部门表（树形自关联）
CREATE TABLE IF NOT EXISTS departments (
    id          BIGSERIAL    PRIMARY KEY,
    org_id      BIGINT       NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    parent_id   BIGINT       NULL REFERENCES departments(id) ON DELETE SET NULL,
    name        VARCHAR(128) NOT NULL,
    code        VARCHAR(64)  NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    sort_order  INT          NOT NULL DEFAULT 0,
    leader_id   BIGINT       NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    status      VARCHAR(16)  NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT departments_status_chk CHECK (status IN ('active', 'disabled')),
    UNIQUE (org_id, code)
);

CREATE INDEX IF NOT EXISTS idx_departments_parent ON departments(parent_id);
CREATE INDEX IF NOT EXISTS idx_departments_org ON departments(org_id);

-- 部门成员
CREATE TABLE IF NOT EXISTS department_members (
    id            BIGSERIAL   PRIMARY KEY,
    department_id BIGINT      NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    admin_id      BIGINT      NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    is_leader     BOOLEAN     NOT NULL DEFAULT FALSE,
    joined_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (department_id, admin_id)
);

CREATE INDEX IF NOT EXISTS idx_dept_members_admin ON department_members(admin_id);
CREATE INDEX IF NOT EXISTS idx_dept_members_dept ON department_members(department_id);

-- 权限扩展：admin_assignments 新增可选 department_id
ALTER TABLE admin_assignments ADD COLUMN IF NOT EXISTS department_id BIGINT NULL REFERENCES departments(id) ON DELETE SET NULL;
