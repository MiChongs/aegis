-- +migrate Down
DROP TABLE IF EXISTS app_splash_ad_events;
DROP TABLE IF EXISTS user_ad_consent_logs;
DROP TABLE IF EXISTS user_ad_consents;
DROP TABLE IF EXISTS app_ad_policies;
