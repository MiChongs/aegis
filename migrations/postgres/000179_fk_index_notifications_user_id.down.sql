-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_notifications_user_id_fk;
