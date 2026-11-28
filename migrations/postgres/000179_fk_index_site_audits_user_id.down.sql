-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_site_audits_user_id_fk;
