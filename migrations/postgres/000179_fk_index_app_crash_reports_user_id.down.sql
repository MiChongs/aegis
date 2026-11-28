-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_app_crash_reports_user_id_fk;
