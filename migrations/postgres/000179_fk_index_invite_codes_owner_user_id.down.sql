-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_invite_codes_owner_user_id_fk;
