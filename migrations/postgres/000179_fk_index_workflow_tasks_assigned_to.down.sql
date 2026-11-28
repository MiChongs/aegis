-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_workflow_tasks_assigned_to_fk;
