-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_device_limit_events_user_id_fk;
