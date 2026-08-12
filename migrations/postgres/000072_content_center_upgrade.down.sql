-- +migrate Down

DROP INDEX IF EXISTS idx_banners_app_active;
DROP INDEX IF EXISTS idx_notices_app_status;
DROP INDEX IF EXISTS idx_notices_window;
DROP INDEX IF EXISTS idx_notices_app_published;

-- 回滚后 type 不再有枚举含义，恢复升级前的默认值。
ALTER TABLE banners ALTER COLUMN type SET DEFAULT 'url';

ALTER TABLE notices
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS view_count,
    DROP COLUMN IF EXISTS published_at,
    DROP COLUMN IF EXISTS end_time,
    DROP COLUMN IF EXISTS start_time,
    DROP COLUMN IF EXISTS summary,
    DROP COLUMN IF EXISTS pinned,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS level,
    DROP COLUMN IF EXISTS type;
