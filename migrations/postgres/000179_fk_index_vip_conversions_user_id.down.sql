-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_vip_conversions_user_id_fk;
