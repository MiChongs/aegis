-- +migrate Up
-- 群发任务一人一条：批量写入用 ON CONFLICT (campaign_id, user_id) WHERE campaign_id IS NOT NULL DO NOTHING，
-- 接手中断的任务、重放同一批都不会重复投递；撤回按 campaign_id 找消息也走它。
-- CONCURRENTLY：notifications 可能很大，普通建索引会在构建期间挡住所有投递。
-- 本文件只能有这一条语句：CONCURRENTLY 不能在事务块里执行，多条语句会被合成一个隐式事务。
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_notifications_campaign_user ON notifications (campaign_id, user_id) WHERE campaign_id IS NOT NULL;
