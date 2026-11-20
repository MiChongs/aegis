-- +migrate Down
DROP TRIGGER IF EXISTS trg_users_number_release ON users;
DROP TRIGGER IF EXISTS trg_users_number_assign ON users;
DROP FUNCTION IF EXISTS aegis_users_number_release();
DROP FUNCTION IF EXISTS aegis_users_number_assign();
DROP FUNCTION IF EXISTS aegis_new_user_number(BIGINT);
DROP FUNCTION IF EXISTS aegis_user_number_is_plain(BIGINT);

ALTER TABLE users DROP CONSTRAINT IF EXISTS ck_users_number_range;
DROP INDEX IF EXISTS uq_users_app_number;
ALTER TABLE users DROP COLUMN IF EXISTS number_changed_at;
ALTER TABLE users DROP COLUMN IF EXISTS number_tier;
ALTER TABLE users DROP COLUMN IF EXISTS number;

DROP TABLE IF EXISTS user_number_draws;
DROP TABLE IF EXISTS user_number_ledger;
DROP TABLE IF EXISTS user_number_rights;
DROP TABLE IF EXISTS user_number_history;
DROP TABLE IF EXISTS user_number_holds;
DROP TABLE IF EXISTS user_number_settings;
