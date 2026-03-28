DELETE FROM admin_assignments WHERE role_key LIKE 'custom_%';
ALTER TABLE admin_assignments ADD CONSTRAINT admin_assignments_role_chk
    CHECK (role_key IN ('platform_admin', 'app_admin', 'app_operator', 'app_auditor', 'app_viewer'));
DROP TABLE IF EXISTS admin_role_permissions;
DROP TABLE IF EXISTS admin_roles;
