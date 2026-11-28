-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_invite_relations_invitee_id_fk;
