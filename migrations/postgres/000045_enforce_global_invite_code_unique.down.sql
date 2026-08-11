DROP INDEX IF EXISTS uq_user_profiles_invite_code;

CREATE INDEX IF NOT EXISTS idx_user_profiles_invite_code
  ON user_profiles(invite_code);
