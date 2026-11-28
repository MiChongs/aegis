-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_lottery_user_states_user_id_fk;
