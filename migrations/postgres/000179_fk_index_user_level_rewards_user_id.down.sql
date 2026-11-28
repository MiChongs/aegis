-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_level_rewards_user_id_fk;
