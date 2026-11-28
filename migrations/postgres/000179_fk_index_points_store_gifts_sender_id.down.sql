-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_points_store_gifts_sender_id_fk;
