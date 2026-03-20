-- +migrate Down
DROP INDEX IF EXISTS idx_experience_transactions_category;
DROP INDEX IF EXISTS idx_experience_transactions_app_time;
DROP INDEX IF EXISTS idx_experience_transactions_user_time;
DROP INDEX IF EXISTS idx_integral_transactions_category;
DROP INDEX IF EXISTS idx_integral_transactions_app_time;
DROP INDEX IF EXISTS idx_integral_transactions_user_time;
DROP TABLE IF EXISTS experience_transactions;
DROP TABLE IF EXISTS integral_transactions;
