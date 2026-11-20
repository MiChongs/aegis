-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS uq_notifications_campaign_user;
