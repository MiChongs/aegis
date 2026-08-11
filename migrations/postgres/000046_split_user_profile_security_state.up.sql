CREATE TABLE IF NOT EXISTS user_password_security_states (
  user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  password_changed_at TIMESTAMPTZ NULL,
  password_expires_at TIMESTAMPTZ NULL,
  password_strength_score INTEGER NULL,
  password_change_required BOOLEAN NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO user_password_security_states (
  user_id,
  password_changed_at,
  password_expires_at,
  password_strength_score,
  password_change_required,
  created_at,
  updated_at
)
SELECT
  p.user_id,
  COALESCE(
    p.password_changed_at,
    CASE
      WHEN NULLIF(p.extra->>'password_changed_at', '') IS NULL THEN NULL
      WHEN (p.extra->>'password_changed_at') ~ '^[0-9]+$' THEN to_timestamp((p.extra->>'password_changed_at')::double precision)
      ELSE (p.extra->>'password_changed_at')::timestamptz
    END
  ),
  COALESCE(
    p.password_expires_at,
    CASE
      WHEN NULLIF(p.extra->>'password_expires_at', '') IS NULL THEN NULL
      WHEN (p.extra->>'password_expires_at') ~ '^[0-9]+$' THEN to_timestamp((p.extra->>'password_expires_at')::double precision)
      ELSE (p.extra->>'password_expires_at')::timestamptz
    END
  ),
  COALESCE(
    p.password_strength_score,
    CASE
      WHEN NULLIF(p.extra->>'password_strength_score', '') IS NULL THEN NULL
      ELSE (p.extra->>'password_strength_score')::integer
    END
  ),
  COALESCE(
    p.password_change_required,
    CASE
      WHEN NULLIF(p.extra->>'password_change_required', '') IS NULL THEN NULL
      ELSE (p.extra->>'password_change_required')::boolean
    END
  ),
  COALESCE(p.updated_at, NOW()),
  COALESCE(p.updated_at, NOW())
FROM user_profiles p
WHERE
  p.password_changed_at IS NOT NULL OR
  p.password_expires_at IS NOT NULL OR
  p.password_strength_score IS NOT NULL OR
  p.password_change_required IS NOT NULL OR
  (p.extra IS NOT NULL AND (
    p.extra ? 'password_changed_at' OR
    p.extra ? 'password_expires_at' OR
    p.extra ? 'password_strength_score' OR
    p.extra ? 'password_change_required'
  ))
ON CONFLICT (user_id) DO UPDATE SET
  password_changed_at = COALESCE(EXCLUDED.password_changed_at, user_password_security_states.password_changed_at),
  password_expires_at = COALESCE(EXCLUDED.password_expires_at, user_password_security_states.password_expires_at),
  password_strength_score = COALESCE(EXCLUDED.password_strength_score, user_password_security_states.password_strength_score),
  password_change_required = COALESCE(EXCLUDED.password_change_required, user_password_security_states.password_change_required),
  updated_at = NOW();

UPDATE user_profiles
SET extra = COALESCE(extra, '{}'::jsonb)
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
WHERE extra IS NOT NULL AND extra <> '{}'::jsonb;

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
  DROP COLUMN IF EXISTS two_factor_enabled;
