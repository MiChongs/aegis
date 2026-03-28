-- 自定义角色定义表
CREATE TABLE IF NOT EXISTS admin_roles (
    id          BIGSERIAL    PRIMARY KEY,
    role_key    VARCHAR(64)  NOT NULL UNIQUE,
    name        VARCHAR(128) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    level       INT          NOT NULL DEFAULT 10,
    scope       VARCHAR(16)  NOT NULL DEFAULT 'app',
    base_role   VARCHAR(64)  NULL,
    created_by  BIGINT       NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT admin_roles_scope_chk CHECK (scope IN ('global', 'app')),
    CONSTRAINT admin_roles_key_prefix CHECK (role_key LIKE 'custom_%'),
    CONSTRAINT admin_roles_level_chk CHECK (level >= 1 AND level < 20)
);

-- 自定义角色权限关联表
CREATE TABLE IF NOT EXISTS admin_role_permissions (
    id          BIGSERIAL    PRIMARY KEY,
    role_key    VARCHAR(64)  NOT NULL REFERENCES admin_roles(role_key) ON DELETE CASCADE,
    permission  VARCHAR(128) NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT admin_role_permissions_unique UNIQUE (role_key, permission)
);

CREATE INDEX IF NOT EXISTS idx_admin_role_permissions_role_key ON admin_role_permissions(role_key);

-- 移除 admin_assignments 的 CHECK 约束，允许自定义角色 key
ALTER TABLE admin_assignments DROP CONSTRAINT IF EXISTS admin_assignments_role_chk;
