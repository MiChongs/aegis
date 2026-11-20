-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_notifications_app_time;
