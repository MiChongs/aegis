DROP TABLE IF EXISTS collaboration_groups;
DROP TABLE IF EXISTS org_app_bindings;
DROP TABLE IF EXISTS org_permission_templates;
DROP TABLE IF EXISTS approval_instances;
DROP TABLE IF EXISTS approval_chains;
ALTER TABLE admin_assignments DROP COLUMN IF EXISTS org_id;
