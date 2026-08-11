ALTER TABLE user_profiles
  ADD COLUMN IF NOT EXISTS two_factor_enabled BOOLEAN NULL,
  ADD COLUMN IF NOT EXISTS two_factor_method VARCHAR(32) NULL,
  ADD COLUMN IF NOT EXISTS two_factor_enabled_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS two_factor_disabled_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS passkey_enabled BOOLEAN NULL,
  ADD COLUMN IF NOT EXISTS passkey_enabled_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS password_expires_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS password_strength_score INTEGER NULL,
  ADD COLUMN IF NOT EXISTS password_change_required BOOLEAN NULL;

UPDATE user_profiles p
SET
  password_changed_at = s.password_changed_at,
  password_expires_at = s.password_expires_at,
  password_strength_score = s.password_strength_score,
  password_change_required = s.password_change_required
FROM user_password_security_states s
WHERE s.user_id = p.user_id;

DROP TABLE IF EXISTS user_password_security_states;
