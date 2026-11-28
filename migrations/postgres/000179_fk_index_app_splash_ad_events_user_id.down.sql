-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_app_splash_ad_events_user_id_fk;
