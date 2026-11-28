-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_invite_milestone_grants_user_id_fk;
