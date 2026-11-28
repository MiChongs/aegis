-- +migrate Up
-- CONCURRENTLY：不挡线上写入；本文件只能有这一条语句（多条语句会合成隐式事务）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_points_store_gifts_recipient_id_fk ON points_store_gifts (recipient_id) WHERE recipient_id IS NOT NULL;
