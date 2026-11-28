-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_app_cloud_storage_users_user_id_fk;
