-- +migrate Up
-- CONCURRENTLY：不挡线上写入；本文件只能有这一条语句（多条语句会合成隐式事务）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_invite_relations_inviter_id_fk ON invite_relations (inviter_id) WHERE inviter_id IS NOT NULL;
