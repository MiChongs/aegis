-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_lottery_draw_ledger_lot_id_fk;
