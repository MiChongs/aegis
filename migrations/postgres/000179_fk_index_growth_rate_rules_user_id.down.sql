-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_growth_rate_rules_user_id_fk;
