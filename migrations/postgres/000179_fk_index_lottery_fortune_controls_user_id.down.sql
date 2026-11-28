-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_lottery_fortune_controls_user_id_fk;
