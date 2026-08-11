DROP INDEX IF EXISTS idx_user_profiles_invite_code;
DROP INDEX IF EXISTS idx_user_profiles_custom_id;
DROP INDEX IF EXISTS idx_user_profiles_register_ip;

ALTER TABLE user_profiles
  DROP COLUMN IF EXISTS password_change_required,
  DROP COLUMN IF EXISTS password_strength_score,
  DROP COLUMN IF EXISTS password_expires_at,
  DROP COLUMN IF EXISTS password_changed_at,
  DROP COLUMN IF EXISTS passkey_enabled_at,
  DROP COLUMN IF EXISTS passkey_enabled,
  DROP COLUMN IF EXISTS two_factor_disabled_at,
  DROP COLUMN IF EXISTS two_factor_enabled_at,
  DROP COLUMN IF EXISTS two_factor_method,
  DROP COLUMN IF EXISTS two_factor_enabled,
  DROP COLUMN IF EXISTS disabled_reason,
  DROP COLUMN IF EXISTS register_time,
  DROP COLUMN IF EXISTS register_city,
  DROP COLUMN IF EXISTS register_province,
  DROP COLUMN IF EXISTS register_isp,
  DROP COLUMN IF EXISTS register_ip,
  DROP COLUMN IF EXISTS parent_invite_account,
  DROP COLUMN IF EXISTS invite_code,
  DROP COLUMN IF EXISTS custom_id_count,
  DROP COLUMN IF EXISTS custom_id,
  DROP COLUMN IF EXISTS mark_code,
  DROP COLUMN IF EXISTS role;
