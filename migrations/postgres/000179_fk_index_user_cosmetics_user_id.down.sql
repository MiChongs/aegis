-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_cosmetics_user_id_fk;
