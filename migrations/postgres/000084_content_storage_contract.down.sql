-- +migrate Down

ALTER TABLE platform_banners
    DROP CONSTRAINT IF EXISTS ck_platform_banners_required,
    DROP CONSTRAINT IF EXISTS ck_platform_banners_window,
    DROP CONSTRAINT IF EXISTS ck_platform_banners_type;

ALTER TABLE notices
    DROP CONSTRAINT IF EXISTS ck_notices_title,
    DROP CONSTRAINT IF EXISTS ck_notices_window,
    DROP CONSTRAINT IF EXISTS ck_notices_status,
    DROP CONSTRAINT IF EXISTS ck_notices_level,
    DROP CONSTRAINT IF EXISTS ck_notices_type;

ALTER TABLE banners
    DROP CONSTRAINT IF EXISTS ck_banners_title,
    DROP CONSTRAINT IF EXISTS ck_banners_window,
    DROP CONSTRAINT IF EXISTS ck_banners_type;

-- 恢复可空。空串不再转回 NULL：那次转换本身就是这轮要消掉的二义性，
-- 回滚只需要让旧代码能继续写 NULL，不需要把数据也退回两种存法。
ALTER TABLE notices
    ALTER COLUMN title DROP NOT NULL,
    ALTER COLUMN title DROP DEFAULT;

ALTER TABLE banners
    ALTER COLUMN header  DROP NOT NULL,
    ALTER COLUMN content DROP NOT NULL,
    ALTER COLUMN url     DROP NOT NULL,
    ALTER COLUMN header  DROP DEFAULT,
    ALTER COLUMN content DROP DEFAULT,
    ALTER COLUMN url     DROP DEFAULT;
