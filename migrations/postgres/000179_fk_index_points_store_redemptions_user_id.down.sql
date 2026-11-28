-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_points_store_redemptions_user_id_fk;
