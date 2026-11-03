-- +migrate Down
ALTER TABLE user_level_records DROP COLUMN IF EXISTS capped_level;
DROP TABLE IF EXISTS growth_events;
DROP TABLE IF EXISTS growth_daily_usage;
DROP TABLE IF EXISTS growth_rate_rules;
