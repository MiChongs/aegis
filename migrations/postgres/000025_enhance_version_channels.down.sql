ALTER TABLE app_version_channels
  DROP COLUMN IF EXISTS priority,
  DROP COLUMN IF EXISTS color,
  DROP COLUMN IF EXISTS level,
  DROP COLUMN IF EXISTS rollout_pct,
  DROP COLUMN IF EXISTS platforms,
  DROP COLUMN IF EXISTS min_version_code,
  DROP COLUMN IF EXISTS max_version_code,
  DROP COLUMN IF EXISTS rules;
