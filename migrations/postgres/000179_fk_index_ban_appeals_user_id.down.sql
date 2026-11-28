-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_ban_appeals_user_id_fk;
