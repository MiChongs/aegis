-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_lottery_draw_lots_user_id_fk;
