-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_login_devices_user_id_fk;
