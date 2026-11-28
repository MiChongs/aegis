-- +migrate Up
-- CONCURRENTLY：不挡线上写入；本文件只能有这一条语句（多条语句会合成隐式事务）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_card_keys_bound_user_id_fk ON card_keys (bound_user_id) WHERE bound_user_id IS NOT NULL;
