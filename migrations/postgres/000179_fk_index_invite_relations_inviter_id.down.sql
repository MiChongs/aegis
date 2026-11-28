-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_invite_relations_inviter_id_fk;
