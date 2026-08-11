-- ============================================================================
-- 组织基础设施：对外 UUID、组织层级、部门物化路径 + 闭包表、组织成员、组织角色
--
-- 设计要点：
--   1. 对外标识一律用 uuid，自增 id 只在库内 JOIN 使用（API/前端不再暴露自增 ID）
--   2. 部门树用「物化路径 + 闭包表」双写：path 做子树范围查询与环检测，
--      closure 做「直接下级 / 第 N 层后代 / 祖先链」这类带 depth 的查询
--   3. 组织成员（org_members）是独立一层：管理员先入组织，再入部门。
--      department_members 通过复合外键强制「部门成员必是该组织成员」，
--      跨组织串人在数据库层面即不可能
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ── 组织：对外 UUID、层级、所有者、配额 ──────────────────────────────────────

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS uuid          UUID        NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN IF NOT EXISTS parent_id     BIGINT      NULL,
    ADD COLUMN IF NOT EXISTS path          TEXT        NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS depth         INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS kind          VARCHAR(32) NOT NULL DEFAULT 'enterprise',
    ADD COLUMN IF NOT EXISTS owner_id      BIGINT      NULL,
    ADD COLUMN IF NOT EXISTS contact_name  VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS contact_email VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS contact_phone VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS industry      VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS region        VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS member_limit  INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS dept_limit    INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS app_limit     INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS expires_at    TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS settings      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS updated_by    BIGINT      NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_organizations_uuid ON organizations(uuid);
CREATE INDEX IF NOT EXISTS idx_organizations_parent ON organizations(parent_id);
CREATE INDEX IF NOT EXISTS idx_organizations_path ON organizations(path text_pattern_ops);
CREATE INDEX IF NOT EXISTS idx_organizations_owner ON organizations(owner_id);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'organizations_parent_fk') THEN
        ALTER TABLE organizations ADD CONSTRAINT organizations_parent_fk
            FOREIGN KEY (parent_id) REFERENCES organizations(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'organizations_owner_fk') THEN
        ALTER TABLE organizations ADD CONSTRAINT organizations_owner_fk
            FOREIGN KEY (owner_id) REFERENCES admin_accounts(id) ON DELETE SET NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'organizations_updated_by_fk') THEN
        ALTER TABLE organizations ADD CONSTRAINT organizations_updated_by_fk
            FOREIGN KEY (updated_by) REFERENCES admin_accounts(id) ON DELETE SET NULL;
    END IF;
END $$;

-- 状态扩容：active / suspended（停用，数据保留可恢复）/ archived（归档只读）
ALTER TABLE organizations DROP CONSTRAINT IF EXISTS organizations_status_chk;
UPDATE organizations SET status = 'suspended' WHERE status = 'disabled';
ALTER TABLE organizations ADD CONSTRAINT organizations_status_chk
    CHECK (status IN ('active', 'suspended', 'archived'));

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'organizations_kind_chk') THEN
        ALTER TABLE organizations ADD CONSTRAINT organizations_kind_chk
            CHECK (kind IN ('enterprise', 'subsidiary', 'branch', 'team', 'partner'));
    END IF;
END $$;

-- 回填：根组织的 owner 取创建者，path 取 /id/
UPDATE organizations SET owner_id = created_by WHERE owner_id IS NULL AND created_by IS NOT NULL;
UPDATE organizations SET path = '/' || id || '/', depth = 0 WHERE path = '';

-- ── 部门：对外 UUID、物化路径、类型 ─────────────────────────────────────────

ALTER TABLE departments
    ADD COLUMN IF NOT EXISTS uuid         UUID        NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN IF NOT EXISTS path         TEXT        NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS depth        INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS kind         VARCHAR(32) NOT NULL DEFAULT 'department',
    ADD COLUMN IF NOT EXISTS member_limit INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS settings     JSONB       NOT NULL DEFAULT '{}'::jsonb;

CREATE UNIQUE INDEX IF NOT EXISTS idx_departments_uuid ON departments(uuid);
CREATE INDEX IF NOT EXISTS idx_departments_path ON departments(path text_pattern_ops);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'departments_kind_chk') THEN
        ALTER TABLE departments ADD CONSTRAINT departments_kind_chk
            CHECK (kind IN ('department', 'team', 'group', 'virtual'));
    END IF;
    -- 复合唯一键：让下游表能用 (id, org_id) 复合外键锁死组织归属
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'departments_id_org_uk') THEN
        ALTER TABLE departments ADD CONSTRAINT departments_id_org_uk UNIQUE (id, org_id);
    END IF;
END $$;

-- 父部门删除策略改为 RESTRICT：原先的 SET NULL 会把整棵子树静默甩到根，
-- 是数据事故；删除策略（禁止 / 级联 / 上移）由服务层显式选择
DO $$
DECLARE
    fk_name TEXT;
BEGIN
    SELECT conname INTO fk_name FROM pg_constraint
     WHERE conrelid = 'departments'::regclass AND contype = 'f'
       AND pg_get_constraintdef(oid) LIKE 'FOREIGN KEY (parent_id)%'
       AND confdeltype <> 'r'
     LIMIT 1;
    IF fk_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE departments DROP CONSTRAINT %I', fk_name);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'departments_parent_fk') THEN
        ALTER TABLE departments ADD CONSTRAINT departments_parent_fk
            FOREIGN KEY (parent_id) REFERENCES departments(id) ON DELETE RESTRICT;
    END IF;
END $$;

-- 回填 path / depth（自顶向下递归）
WITH RECURSIVE tree AS (
    SELECT id, '/' || id || '/' AS full_path, 0 AS lvl
      FROM departments WHERE parent_id IS NULL
    UNION ALL
    SELECT d.id, t.full_path || d.id || '/', t.lvl + 1
      FROM departments d JOIN tree t ON d.parent_id = t.id
)
UPDATE departments d SET path = t.full_path, depth = t.lvl
  FROM tree t WHERE d.id = t.id AND d.path = '';

-- ── 部门闭包表 ─────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS department_closure (
    ancestor_id   BIGINT NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    descendant_id BIGINT NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    depth         INT    NOT NULL,
    PRIMARY KEY (ancestor_id, descendant_id)
);
CREATE INDEX IF NOT EXISTS idx_dept_closure_descendant ON department_closure(descendant_id, depth);
CREATE INDEX IF NOT EXISTS idx_dept_closure_ancestor_depth ON department_closure(ancestor_id, depth);

-- 回填闭包：祖先的 path 是后代 path 的前缀
INSERT INTO department_closure (ancestor_id, descendant_id, depth)
SELECT a.id, d.id, d.depth - a.depth
  FROM departments d
  JOIN departments a ON d.path LIKE a.path || '%' AND a.org_id = d.org_id
ON CONFLICT DO NOTHING;

-- ── 组织成员 ───────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS org_members (
    id              BIGSERIAL   PRIMARY KEY,
    uuid            UUID        NOT NULL DEFAULT gen_random_uuid(),
    org_id          BIGINT      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    admin_id        BIGINT      NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    org_role        VARCHAR(32) NOT NULL DEFAULT 'member',
    primary_dept_id BIGINT      NULL REFERENCES departments(id) ON DELETE SET NULL,
    employee_no     VARCHAR(64) NOT NULL DEFAULT '',
    title           VARCHAR(128) NOT NULL DEFAULT '',
    status          VARCHAR(16) NOT NULL DEFAULT 'active',
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at         TIMESTAMPTZ NULL,
    invited_by      BIGINT      NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT org_members_role_chk   CHECK (org_role IN ('owner', 'admin', 'manager', 'member', 'viewer')),
    CONSTRAINT org_members_status_chk CHECK (status IN ('active', 'suspended', 'left')),
    CONSTRAINT org_members_unique UNIQUE (org_id, admin_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_org_members_uuid ON org_members(uuid);
CREATE INDEX IF NOT EXISTS idx_org_members_admin ON org_members(admin_id, status);
CREATE INDEX IF NOT EXISTS idx_org_members_org_role ON org_members(org_id, org_role);

-- 每个组织至多一个 owner
CREATE UNIQUE INDEX IF NOT EXISTS idx_org_members_single_owner
    ON org_members(org_id) WHERE org_role = 'owner';

-- 回填：现有部门成员补进组织成员；组织创建者提升为 owner
INSERT INTO org_members (org_id, admin_id, org_role, joined_at)
SELECT d.org_id, dm.admin_id, 'member', MIN(dm.joined_at)
  FROM department_members dm JOIN departments d ON d.id = dm.department_id
 GROUP BY d.org_id, dm.admin_id
ON CONFLICT (org_id, admin_id) DO NOTHING;

INSERT INTO org_members (org_id, admin_id, org_role)
SELECT id, owner_id, 'owner' FROM organizations WHERE owner_id IS NOT NULL
ON CONFLICT (org_id, admin_id) DO UPDATE SET org_role = 'owner';

-- ── 部门成员：冗余 org_id，用复合外键锁死组织归属 ──────────────────────────

ALTER TABLE department_members ADD COLUMN IF NOT EXISTS org_id BIGINT NULL;

UPDATE department_members dm SET org_id = d.org_id
  FROM departments d WHERE d.id = dm.department_id AND dm.org_id IS NULL;

-- 回填不到组织的孤儿行（部门已删）直接清理，否则加不上 NOT NULL
DELETE FROM department_members WHERE org_id IS NULL;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_name = 'department_members' AND column_name = 'org_id'
                  AND is_nullable = 'YES') THEN
        ALTER TABLE department_members ALTER COLUMN org_id SET NOT NULL;
    END IF;
    -- 部门必属于该组织
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_members_dept_org_fk') THEN
        ALTER TABLE department_members ADD CONSTRAINT department_members_dept_org_fk
            FOREIGN KEY (department_id, org_id) REFERENCES departments(id, org_id) ON DELETE CASCADE;
    END IF;
    -- 成员必是该组织成员
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_members_org_member_fk') THEN
        ALTER TABLE department_members ADD CONSTRAINT department_members_org_member_fk
            FOREIGN KEY (org_id, admin_id) REFERENCES org_members(org_id, admin_id) ON DELETE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_dept_members_org ON department_members(org_id, admin_id);

-- ── 组织角色（Casbin 组织域的策略来源）────────────────────────────────────

CREATE TABLE IF NOT EXISTS org_roles (
    id          BIGSERIAL   PRIMARY KEY,
    uuid        UUID        NOT NULL DEFAULT gen_random_uuid(),
    org_id      BIGINT      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role_key    VARCHAR(64) NOT NULL,
    name        VARCHAR(128) NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    permissions TEXT[]      NOT NULL DEFAULT '{}',
    is_builtin  BOOLEAN     NOT NULL DEFAULT FALSE,
    created_by  BIGINT      NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT org_roles_unique UNIQUE (org_id, role_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_org_roles_uuid ON org_roles(uuid);

CREATE TABLE IF NOT EXISTS org_role_members (
    id            BIGSERIAL   PRIMARY KEY,
    org_role_id   BIGINT      NOT NULL REFERENCES org_roles(id) ON DELETE CASCADE,
    admin_id      BIGINT      NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    scope_dept_id BIGINT      NULL REFERENCES departments(id) ON DELETE CASCADE,
    granted_by    BIGINT      NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    granted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_org_role_members_unique
    ON org_role_members(org_role_id, admin_id, COALESCE(scope_dept_id, 0));
CREATE INDEX IF NOT EXISTS idx_org_role_members_admin ON org_role_members(admin_id);

-- ── 岗位：对外 UUID ────────────────────────────────────────────────────────

ALTER TABLE positions ADD COLUMN IF NOT EXISTS uuid UUID NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_uuid ON positions(uuid);

-- ── 应用归属组织 ───────────────────────────────────────────────────────────

ALTER TABLE apps ADD COLUMN IF NOT EXISTS org_id BIGINT NULL;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'apps_org_fk') THEN
        ALTER TABLE apps ADD CONSTRAINT apps_org_fk
            FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE SET NULL;
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_apps_org ON apps(org_id);

-- ── 邀请：升级为「组织级邀请」，部门可选 ───────────────────────────────────

ALTER TABLE department_invitations
    ADD COLUMN IF NOT EXISTS uuid     UUID        NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN IF NOT EXISTS org_id   BIGINT      NULL REFERENCES organizations(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS org_role VARCHAR(32) NOT NULL DEFAULT 'member';

UPDATE department_invitations i SET org_id = d.org_id
  FROM departments d WHERE d.id = i.department_id AND i.org_id IS NULL;
DELETE FROM department_invitations WHERE org_id IS NULL;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_name = 'department_invitations' AND column_name = 'org_id'
                  AND is_nullable = 'YES') THEN
        ALTER TABLE department_invitations ALTER COLUMN org_id SET NOT NULL;
    END IF;
END $$;

ALTER TABLE department_invitations ALTER COLUMN department_id DROP NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_dept_invitations_uuid ON department_invitations(uuid);

-- 唯一约束改为「同组织 + 同部门（可空）+ 同被邀人」只允许一条 pending
DROP INDEX IF EXISTS idx_dept_inv_pending_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_org_inv_pending_unique
    ON department_invitations(org_id, COALESCE(department_id, 0), invitee_id) WHERE status = 'pending';

-- ── 审批 / 权限模板 / 协作组：补齐对外 UUID ────────────────────────────────

ALTER TABLE approval_chains          ADD COLUMN IF NOT EXISTS uuid UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE approval_instances       ADD COLUMN IF NOT EXISTS uuid UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE org_permission_templates ADD COLUMN IF NOT EXISTS uuid UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE collaboration_groups     ADD COLUMN IF NOT EXISTS uuid UUID NOT NULL DEFAULT gen_random_uuid();

CREATE UNIQUE INDEX IF NOT EXISTS idx_approval_chains_uuid    ON approval_chains(uuid);
CREATE UNIQUE INDEX IF NOT EXISTS idx_approval_instances_uuid ON approval_instances(uuid);
CREATE UNIQUE INDEX IF NOT EXISTS idx_org_perm_templates_uuid ON org_permission_templates(uuid);
CREATE UNIQUE INDEX IF NOT EXISTS idx_collab_groups_uuid      ON collaboration_groups(uuid);

-- 同一组织 + 同一触发场景只允许一条启用中的审批链，
-- 否则「哪条生效」取决于查询顺序，配置者无从判断
CREATE UNIQUE INDEX IF NOT EXISTS idx_approval_chains_active_trigger
    ON approval_chains(org_id, trigger_type) WHERE is_active;

-- ── 组织操作留痕 ───────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS org_activity_logs (
    id          BIGSERIAL   PRIMARY KEY,
    org_id      BIGINT      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    actor_id    BIGINT      NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    action      VARCHAR(64) NOT NULL,
    target_type VARCHAR(32) NOT NULL DEFAULT '',
    target_id   VARCHAR(64) NOT NULL DEFAULT '',
    summary     TEXT        NOT NULL DEFAULT '',
    detail      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_org_activity_org_time ON org_activity_logs(org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_org_activity_actor ON org_activity_logs(actor_id, created_at DESC);

-- ── admin_assignments 组织维度索引 ─────────────────────────────────────────

CREATE INDEX IF NOT EXISTS idx_admin_assignments_org ON admin_assignments(org_id);
