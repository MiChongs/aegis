-- +migrate Down
DROP TABLE IF EXISTS app_cloud_item_revisions;
DROP TABLE IF EXISTS app_cloud_items;
DROP TABLE IF EXISTS app_cloud_storage_users;
DROP TABLE IF EXISTS app_cloud_storage_configs;
