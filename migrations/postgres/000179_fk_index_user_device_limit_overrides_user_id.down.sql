-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_device_limit_overrides_user_id_fk;
