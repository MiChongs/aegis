-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_level_events_user_id_fk;
