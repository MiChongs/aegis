ALTER TABLE user_profiles
  ADD COLUMN IF NOT EXISTS role VARCHAR(32) NULL,
  ADD COLUMN IF NOT EXISTS mark_code VARCHAR(128) NULL,
  ADD COLUMN IF NOT EXISTS custom_id VARCHAR(128) NULL,
  ADD COLUMN IF NOT EXISTS custom_id_count INTEGER NULL,
  ADD COLUMN IF NOT EXISTS invite_code VARCHAR(128) NULL,
  ADD COLUMN IF NOT EXISTS parent_invite_account VARCHAR(128) NULL,
  ADD COLUMN IF NOT EXISTS register_ip VARCHAR(64) NULL,
  ADD COLUMN IF NOT EXISTS register_isp VARCHAR(64) NULL,
  ADD COLUMN IF NOT EXISTS register_province VARCHAR(64) NULL,
  ADD COLUMN IF NOT EXISTS register_city VARCHAR(64) NULL,
  ADD COLUMN IF NOT EXISTS register_time TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS disabled_reason TEXT NULL,
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

CREATE INDEX IF NOT EXISTS idx_user_profiles_register_ip ON user_profiles(register_ip);
CREATE INDEX IF NOT EXISTS idx_user_profiles_custom_id ON user_profiles(custom_id);
CREATE INDEX IF NOT EXISTS idx_user_profiles_invite_code ON user_profiles(invite_code);

UPDATE user_profiles
SET
  phone = COALESCE(NULLIF(phone, ''), NULLIF(extra->>'phone', ''), ''),
  role = COALESCE(role, NULLIF(extra->>'role', '')),
  mark_code = COALESCE(mark_code, NULLIF(extra->>'markcode', '')),
  custom_id = COALESCE(custom_id, NULLIF(extra->>'custom_id', '')),
  custom_id_count = COALESCE(
    custom_id_count,
    CASE
      WHEN NULLIF(extra->>'custom_id_count', '') IS NULL THEN NULL
      ELSE (extra->>'custom_id_count')::integer
    END
  ),
  invite_code = COALESCE(invite_code, NULLIF(extra->>'invite_code', '')),
  parent_invite_account = COALESCE(parent_invite_account, NULLIF(extra->>'parent_invite_account', '')),
  register_ip = COALESCE(register_ip, NULLIF(extra->>'register_ip', '')),
  register_isp = COALESCE(register_isp, NULLIF(extra->>'register_isp', '')),
  register_province = COALESCE(register_province, NULLIF(extra->>'register_province', '')),
  register_city = COALESCE(register_city, NULLIF(extra->>'register_city', '')),
  register_time = COALESCE(
    register_time,
    CASE
      WHEN NULLIF(extra->>'register_time', '') IS NULL THEN NULL
      WHEN (extra->>'register_time') ~ '^[0-9]+$' THEN to_timestamp((extra->>'register_time')::double precision)
      ELSE (extra->>'register_time')::timestamptz
    END
  ),
  disabled_reason = COALESCE(disabled_reason, NULLIF(extra->>'disabled_reason', '')),
  two_factor_enabled = COALESCE(
    two_factor_enabled,
    CASE
      WHEN NULLIF(extra->>'two_factor_enabled', '') IS NULL THEN NULL
      ELSE (extra->>'two_factor_enabled')::boolean
    END
  ),
  two_factor_method = COALESCE(two_factor_method, NULLIF(extra->>'two_factor_method', '')),
  two_factor_enabled_at = COALESCE(
    two_factor_enabled_at,
    CASE
      WHEN NULLIF(extra->>'two_factor_enabled_at', '') IS NULL THEN NULL
      WHEN (extra->>'two_factor_enabled_at') ~ '^[0-9]+$' THEN to_timestamp((extra->>'two_factor_enabled_at')::double precision)
      ELSE (extra->>'two_factor_enabled_at')::timestamptz
    END
  ),
  two_factor_disabled_at = COALESCE(
    two_factor_disabled_at,
    CASE
      WHEN NULLIF(extra->>'two_factor_disabled_at', '') IS NULL THEN NULL
      WHEN (extra->>'two_factor_disabled_at') ~ '^[0-9]+$' THEN to_timestamp((extra->>'two_factor_disabled_at')::double precision)
      ELSE (extra->>'two_factor_disabled_at')::timestamptz
    END
  ),
  passkey_enabled = COALESCE(
    passkey_enabled,
    CASE
      WHEN NULLIF(extra->>'passkey_enabled', '') IS NULL THEN NULL
      ELSE (extra->>'passkey_enabled')::boolean
    END
  ),
  passkey_enabled_at = COALESCE(
    passkey_enabled_at,
    CASE
      WHEN NULLIF(extra->>'passkey_enabled_at', '') IS NULL THEN NULL
      WHEN (extra->>'passkey_enabled_at') ~ '^[0-9]+$' THEN to_timestamp((extra->>'passkey_enabled_at')::double precision)
      ELSE (extra->>'passkey_enabled_at')::timestamptz
    END
  ),
  password_changed_at = COALESCE(
    password_changed_at,
    CASE
      WHEN NULLIF(extra->>'password_changed_at', '') IS NULL THEN NULL
      WHEN (extra->>'password_changed_at') ~ '^[0-9]+$' THEN to_timestamp((extra->>'password_changed_at')::double precision)
      ELSE (extra->>'password_changed_at')::timestamptz
    END
  ),
  password_expires_at = COALESCE(
    password_expires_at,
    CASE
      WHEN NULLIF(extra->>'password_expires_at', '') IS NULL THEN NULL
      WHEN (extra->>'password_expires_at') ~ '^[0-9]+$' THEN to_timestamp((extra->>'password_expires_at')::double precision)
      ELSE (extra->>'password_expires_at')::timestamptz
    END
  ),
  password_strength_score = COALESCE(
    password_strength_score,
    CASE
      WHEN NULLIF(extra->>'password_strength_score', '') IS NULL THEN NULL
      ELSE (extra->>'password_strength_score')::integer
    END
  ),
  password_change_required = COALESCE(
    password_change_required,
    CASE
      WHEN NULLIF(extra->>'password_change_required', '') IS NULL THEN NULL
      ELSE (extra->>'password_change_required')::boolean
    END
  )
WHERE extra IS NOT NULL AND extra <> '{}'::jsonb;

UPDATE users u
SET vip_expire_at = CASE
  WHEN p.extra ? 'legacy_vip_time' AND NULLIF(p.extra->>'legacy_vip_time', '') IS NOT NULL
    THEN to_timestamp((p.extra->>'legacy_vip_time')::double precision)
  ELSE u.vip_expire_at
END
FROM user_profiles p
WHERE p.user_id = u.id
  AND u.vip_expire_at IS NULL
  AND p.extra ? 'legacy_vip_time';

UPDATE user_profiles
SET extra = COALESCE(extra, '{}'::jsonb)
  - 'phone'
  - 'role'
  - 'markcode'
  - 'integral'
  - 'experience'
  - 'register_ip'
  - 'register_time'
  - 'register_province'
  - 'register_city'
  - 'register_isp'
  - 'disabled_reason'
  - 'parent_invite_account'
  - 'invite_code'
  - 'custom_id'
  - 'custom_id_count'
  - 'two_factor_enabled'
  - 'two_factor_method'
  - 'two_factor_enabled_at'
  - 'two_factor_disabled_at'
  - 'passkey_enabled'
  - 'passkey_enabled_at'
  - 'password_changed_at'
  - 'password_expires_at'
  - 'password_change_required'
  - 'password_strength_score'
  - 'legacy_vip_time'
WHERE extra IS NOT NULL AND extra <> '{}'::jsonb;
