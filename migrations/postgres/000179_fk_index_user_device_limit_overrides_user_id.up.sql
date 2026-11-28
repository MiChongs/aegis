-- +migrate Up
-- CONCURRENTLY：不挡线上写入；本文件只能有这一条语句（多条语句会合成隐式事务）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_device_limit_overrides_user_id_fk ON user_device_limit_overrides (user_id);
