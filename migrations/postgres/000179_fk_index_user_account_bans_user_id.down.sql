-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_account_bans_user_id_fk;
