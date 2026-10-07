-- +migrate Down
DROP INDEX IF EXISTS idx_admin_accounts_previous_lower;
DROP INDEX IF EXISTS uq_admin_accounts_account_lower;
ALTER TABLE admin_accounts
    DROP COLUMN IF EXISTS account_changed_at,
    DROP COLUMN IF EXISTS previous_account,
    DROP COLUMN IF EXISTS password_changed_at;
