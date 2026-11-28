-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_ad_consents_user_id_fk;
