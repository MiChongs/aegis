-- 回滚组织基础设施

DROP TABLE IF EXISTS org_activity_logs;
DROP TABLE IF EXISTS org_role_members;
DROP TABLE IF EXISTS org_roles;
DROP TABLE IF EXISTS department_closure;

ALTER TABLE department_members DROP CONSTRAINT IF EXISTS department_members_org_member_fk;
ALTER TABLE department_members DROP CONSTRAINT IF EXISTS department_members_dept_org_fk;
ALTER TABLE department_members DROP COLUMN IF EXISTS org_id;

DROP TABLE IF EXISTS org_members;

DROP INDEX IF EXISTS idx_org_inv_pending_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_dept_inv_pending_unique
    ON department_invitations(department_id, invitee_id) WHERE status = 'pending';
ALTER TABLE department_invitations
    DROP COLUMN IF EXISTS uuid,
    DROP COLUMN IF EXISTS org_id,
    DROP COLUMN IF EXISTS org_role;

ALTER TABLE apps DROP CONSTRAINT IF EXISTS apps_org_fk;
ALTER TABLE apps DROP COLUMN IF EXISTS org_id;

ALTER TABLE positions DROP COLUMN IF EXISTS uuid;

ALTER TABLE departments DROP CONSTRAINT IF EXISTS departments_parent_fk;
ALTER TABLE departments DROP CONSTRAINT IF EXISTS departments_id_org_uk;
ALTER TABLE departments DROP CONSTRAINT IF EXISTS departments_kind_chk;
ALTER TABLE departments
    DROP COLUMN IF EXISTS uuid,
    DROP COLUMN IF EXISTS path,
    DROP COLUMN IF EXISTS depth,
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS member_limit,
    DROP COLUMN IF EXISTS settings;
ALTER TABLE departments ADD CONSTRAINT departments_parent_id_fkey
    FOREIGN KEY (parent_id) REFERENCES departments(id) ON DELETE SET NULL;

ALTER TABLE organizations DROP CONSTRAINT IF EXISTS organizations_parent_fk;
ALTER TABLE organizations DROP CONSTRAINT IF EXISTS organizations_owner_fk;
ALTER TABLE organizations DROP CONSTRAINT IF EXISTS organizations_updated_by_fk;
ALTER TABLE organizations DROP CONSTRAINT IF EXISTS organizations_kind_chk;
ALTER TABLE organizations DROP CONSTRAINT IF EXISTS organizations_status_chk;
UPDATE organizations SET status = 'disabled' WHERE status IN ('suspended', 'archived');
ALTER TABLE organizations ADD CONSTRAINT organizations_status_chk
    CHECK (status IN ('active', 'disabled'));
ALTER TABLE organizations
    DROP COLUMN IF EXISTS uuid,
    DROP COLUMN IF EXISTS parent_id,
    DROP COLUMN IF EXISTS path,
    DROP COLUMN IF EXISTS depth,
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS owner_id,
    DROP COLUMN IF EXISTS contact_name,
    DROP COLUMN IF EXISTS contact_email,
    DROP COLUMN IF EXISTS contact_phone,
    DROP COLUMN IF EXISTS industry,
    DROP COLUMN IF EXISTS region,
    DROP COLUMN IF EXISTS member_limit,
    DROP COLUMN IF EXISTS dept_limit,
    DROP COLUMN IF EXISTS app_limit,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS settings,
    DROP COLUMN IF EXISTS updated_by;
