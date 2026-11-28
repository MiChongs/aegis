-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_app_cloud_items_user_id_fk;
