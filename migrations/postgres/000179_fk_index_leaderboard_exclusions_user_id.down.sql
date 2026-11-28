-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_leaderboard_exclusions_user_id_fk;
