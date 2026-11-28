-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_lottery_risk_events_user_id_fk;
