-- +migrate Down
DROP TABLE IF EXISTS lottery_risk_keys;
DROP TABLE IF EXISTS lottery_activity_daily;
DROP TABLE IF EXISTS lottery_activity_deltas;
DROP TABLE IF EXISTS lottery_prize_stock;
DROP TABLE IF EXISTS risk_trail_events;
DELETE FROM schema_backfills WHERE name LIKE 'lottery_%';
ALTER TABLE lottery_user_states
    DROP COLUMN IF EXISTS counted_draws,
    DROP COLUMN IF EXISTS day_start,
    DROP COLUMN IF EXISTS free_today,
    DROP COLUMN IF EXISTS points_today,
    DROP COLUMN IF EXISTS draws_today,
    DROP COLUMN IF EXISTS wins_today,
    DROP COLUMN IF EXISTS wins_total,
    DROP COLUMN IF EXISTS prize_wins,
    DROP COLUMN IF EXISTS prize_wins_today,
    DROP COLUMN IF EXISTS recent,
    DROP COLUMN IF EXISTS burst_start,
    DROP COLUMN IF EXISTS burst_count,
    DROP COLUMN IF EXISTS burst_prev;
CREATE INDEX IF NOT EXISTS idx_lottery_draws_user ON lottery_draws(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_lottery_draws_activity_user ON lottery_draws(activity_id, user_id);
CREATE INDEX IF NOT EXISTS idx_lottery_draws_user_prize ON lottery_draws(activity_id, user_id, prize_id, created_at);
CREATE INDEX IF NOT EXISTS idx_lottery_draws_ip ON lottery_draws(activity_id, client_ip, created_at) WHERE client_ip IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_lottery_draws_device ON lottery_draws(activity_id, device_id, created_at) WHERE device_id IS NOT NULL;
