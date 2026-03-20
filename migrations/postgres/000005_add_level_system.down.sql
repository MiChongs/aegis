-- +migrate Down
DROP INDEX IF EXISTS idx_user_level_records_app_user;
DROP INDEX IF EXISTS idx_user_level_records_app_level_exp;
DROP INDEX IF EXISTS idx_user_levels_active_sort;
DROP TABLE IF EXISTS user_level_records;
DROP TABLE IF EXISTS user_levels;
