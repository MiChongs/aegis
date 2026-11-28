-- +migrate Up
-- CONCURRENTLY：不挡线上写入；本文件只能有这一条语句（多条语句会合成隐式事务）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workflow_instances_started_by_fk ON workflow_instances (started_by) WHERE started_by IS NOT NULL;
