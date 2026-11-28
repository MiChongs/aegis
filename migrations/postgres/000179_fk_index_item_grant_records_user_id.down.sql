-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_item_grant_records_user_id_fk;
