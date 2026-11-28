-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_points_store_gifts_recipient_id_fk;
