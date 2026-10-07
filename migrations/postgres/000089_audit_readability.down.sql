-- +migrate Down
DROP INDEX IF EXISTS idx_admin_audit_catalog;
DROP INDEX IF EXISTS idx_admin_audit_app_time;
DROP INDEX IF EXISTS idx_admin_audit_kind_time;
ALTER TABLE admin_audit_logs
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS app_id,
    DROP COLUMN IF EXISTS app_name,
    DROP COLUMN IF EXISTS target_name,
    DROP COLUMN IF EXISTS catalog_rev;
