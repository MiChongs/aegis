-- 管理员分级与角色模型升级（docs/admin-permissions.md）。
--
-- 本文件随每次启动整份重放，全部语句必须幂等。

-- ── 1. 系统管理员：超级管理员之上的一档 ──
--
-- 系统管理员一定也是超级管理员（is_super_admin 继续承担「不受权限点约束」的语义，
-- 既有的几十处超管判定无需改动）；多出来的这一列只回答一件事：
-- 谁能管理超级管理员、谁不能被任何超级管理员停用。
ALTER TABLE admin_accounts ADD COLUMN IF NOT EXISTS is_system_admin BOOLEAN NOT NULL DEFAULT FALSE;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'admin_accounts_system_implies_super_chk') THEN
        ALTER TABLE admin_accounts
            ADD CONSTRAINT admin_accounts_system_implies_super_chk CHECK (NOT is_system_admin OR is_super_admin);
    END IF;
END $$;

-- 存量部署：没有任何系统管理员时，把最早创建的活跃超级管理员提升为系统管理员。
-- 引导超管（ADMIN_BOOTSTRAP_ACCOUNT）总是第一个被创建的账号，因此通常就是它；
-- 启动时 EnsureSystemAdmin 还会再兜一次底（配置了引导账号时优先提升引导账号）。
UPDATE admin_accounts
SET is_system_admin = TRUE, updated_at = NOW()
WHERE id = (
    SELECT id FROM admin_accounts
    WHERE is_super_admin = TRUE AND status = 'active'
    ORDER BY id ASC
    LIMIT 1
)
AND NOT EXISTS (SELECT 1 FROM admin_accounts WHERE is_system_admin = TRUE);

CREATE INDEX IF NOT EXISTS idx_admin_accounts_tier ON admin_accounts (is_system_admin, is_super_admin, status);

-- ── 2. 自定义角色：状态、排序、数据权限 ──
--
-- data_scope 决定应用级角色在哪些应用里生效（对标若依的数据权限）：
--   assigned  按授权时绑定的应用（原有行为）
--   all       全部应用（不含平台级权限）
--   created   该管理员自己创建的应用
--   custom    角色上指定的应用（data_app_ids）
ALTER TABLE admin_roles ADD COLUMN IF NOT EXISTS status VARCHAR(16) NOT NULL DEFAULT 'active';
ALTER TABLE admin_roles ADD COLUMN IF NOT EXISTS sort_order INT NOT NULL DEFAULT 0;
ALTER TABLE admin_roles ADD COLUMN IF NOT EXISTS data_scope VARCHAR(16) NOT NULL DEFAULT 'assigned';
ALTER TABLE admin_roles ADD COLUMN IF NOT EXISTS data_app_ids BIGINT[] NOT NULL DEFAULT '{}';
ALTER TABLE admin_roles ADD COLUMN IF NOT EXISTS updated_by BIGINT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'admin_roles_status_chk') THEN
        ALTER TABLE admin_roles ADD CONSTRAINT admin_roles_status_chk CHECK (status IN ('active', 'disabled'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'admin_roles_data_scope_chk') THEN
        ALTER TABLE admin_roles ADD CONSTRAINT admin_roles_data_scope_chk
            CHECK (data_scope IN ('assigned', 'all', 'created', 'custom'));
    END IF;
END $$;

-- ── 3. 角色权限：允许 / 禁止 ──
--
-- 角色编辑器的每个节点有三态（未设置 / 允许 / 禁止），禁止项落成 deny 策略，
-- 压倒该角色从任何来源（含继承）得到的允许。
ALTER TABLE admin_role_permissions ADD COLUMN IF NOT EXISTS effect VARCHAR(8) NOT NULL DEFAULT 'allow';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'admin_role_permissions_effect_chk') THEN
        ALTER TABLE admin_role_permissions ADD CONSTRAINT admin_role_permissions_effect_chk
            CHECK (effect IN ('allow', 'deny'));
    END IF;
END $$;
