-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_workflow_instances_started_by_fk;
