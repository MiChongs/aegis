-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_ad_consent_logs_user_id_fk;
