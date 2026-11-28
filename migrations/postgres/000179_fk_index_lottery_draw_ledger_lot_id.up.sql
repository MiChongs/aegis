-- +migrate Up
-- CONCURRENTLY：不挡线上写入；本文件只能有这一条语句（多条语句会合成隐式事务）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lottery_draw_ledger_lot_id_fk ON lottery_draw_ledger (lot_id) WHERE lot_id IS NOT NULL;
