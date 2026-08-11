UPDATE user_profiles
SET invite_code = NULLIF(BTRIM(invite_code), '')
WHERE invite_code IS NOT NULL;

WITH duplicate_codes AS (
  SELECT
    user_id,
    ROW_NUMBER() OVER (PARTITION BY invite_code ORDER BY user_id) AS rn
  FROM user_profiles
  WHERE invite_code IS NOT NULL
    AND invite_code <> ''
)
UPDATE user_profiles AS p
SET invite_code = UPPER(SUBSTRING(REPLACE(gen_random_uuid()::text, '-', '') || LPAD(TO_HEX(p.user_id), 16, '0') FROM 1 FOR 24))
FROM duplicate_codes AS d
WHERE p.user_id = d.user_id
  AND d.rn > 1;

DROP INDEX IF EXISTS idx_user_profiles_invite_code;

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_profiles_invite_code
  ON user_profiles(invite_code)
  WHERE invite_code IS NOT NULL AND invite_code <> '';
