-- +migrate Up
-- CONCURRENTLY：不挡线上写入；本文件只能有这一条语句（多条语句会合成隐式事务）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_feedback_rewards_user_id_fk ON feedback_rewards (user_id) WHERE user_id IS NOT NULL;
