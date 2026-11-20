-- +migrate Down
DROP INDEX IF EXISTS uq_notifications_campaign_user;
DROP INDEX IF EXISTS idx_notifications_app_time;
ALTER TABLE notifications DROP COLUMN IF EXISTS campaign_id;
DROP TABLE IF EXISTS notification_campaigns;
