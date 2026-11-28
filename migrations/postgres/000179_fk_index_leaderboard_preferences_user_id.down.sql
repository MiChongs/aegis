-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_leaderboard_preferences_user_id_fk;
