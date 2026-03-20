-- +migrate Down
DROP INDEX IF EXISTS idx_notification_receipts_user_status;
DROP INDEX IF EXISTS idx_notifications_app_status_time;
DROP INDEX IF EXISTS idx_notifications_app_user_time;
DROP TABLE IF EXISTS notification_receipts;
DROP TABLE IF EXISTS notifications;
