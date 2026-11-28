-- +migrate Down
DROP TABLE IF EXISTS app_cloud_sync_handoffs;
DROP TABLE IF EXISTS app_cloud_sync_devices;
DROP TABLE IF EXISTS app_cloud_sync_records;
ALTER TABLE app_cloud_storage_users DROP COLUMN IF EXISTS last_sync_at;
ALTER TABLE app_cloud_storage_users DROP COLUMN IF EXISTS sync_records;
ALTER TABLE app_cloud_storage_users DROP COLUMN IF EXISTS sync_bytes;
ALTER TABLE app_cloud_storage_users DROP COLUMN IF EXISTS sync_floor_seq;
ALTER TABLE app_cloud_storage_users DROP COLUMN IF EXISTS sync_seq;
ALTER TABLE app_cloud_storage_configs DROP COLUMN IF EXISTS plans;
