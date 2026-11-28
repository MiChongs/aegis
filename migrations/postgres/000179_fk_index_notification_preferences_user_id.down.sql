-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_notification_preferences_user_id_fk;
