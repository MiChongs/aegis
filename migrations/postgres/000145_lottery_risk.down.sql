-- +migrate Down
DROP TABLE IF EXISTS lottery_risk_events;
DROP INDEX IF EXISTS idx_user_login_devices_device;
DROP INDEX IF EXISTS idx_lottery_draws_app_ip;
DROP INDEX IF EXISTS idx_lottery_draws_app_device;
DROP INDEX IF EXISTS idx_lottery_draws_app_user;
DROP INDEX IF EXISTS idx_lottery_draws_prize_name_trgm;
DROP INDEX IF EXISTS idx_lottery_draws_risk_rules;
DROP INDEX IF EXISTS idx_lottery_draws_app_risk;
DROP INDEX IF EXISTS idx_lottery_draws_risk;
ALTER TABLE lottery_draws DROP COLUMN IF EXISTS risk_action, DROP COLUMN IF EXISTS risk_rules;
