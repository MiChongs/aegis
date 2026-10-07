-- +migrate Down
ALTER TABLE app_versions DROP CONSTRAINT IF EXISTS ck_app_versions_release;
DROP INDEX IF EXISTS idx_app_versions_release_active;
DROP TABLE IF EXISTS app_version_daily_stats;
DROP TABLE IF EXISTS app_version_assets;
ALTER TABLE app_version_channels DROP COLUMN IF EXISTS self_join;
ALTER TABLE app_versions
    DROP COLUMN IF EXISTS title,
    DROP COLUMN IF EXISTS notes_summary,
    DROP COLUMN IF EXISTS visibility,
    DROP COLUMN IF EXISTS targeting,
    DROP COLUMN IF EXISTS rollout_pct,
    DROP COLUMN IF EXISTS min_supported_code,
    DROP COLUMN IF EXISTS publish_at,
    DROP COLUMN IF EXISTS published_at,
    DROP COLUMN IF EXISTS created_by;
