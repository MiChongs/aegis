-- +migrate Up
-- 内容板块（应用级 banners / notices + 平台级 platform_banners）的存储契约收敛。
--
-- 这个模块的三条不变量此前**只写在 Go 里**，数据库对它们一无所知：
--   1. type / level / status 的取值白名单
--   2. 投放窗口必须 end_time > start_time
--   3. 标题必填
-- 任何绕过 service 的写入（SQL 导入、运维手改、后来新加的代码路径）都能存进
-- 一条客户端渲染不出来、或者永远不生效的记录，而且一声不响 —— 表现是
-- 「后台明明建好了，客户端就是不显示」，从日志到接口返回都看不出哪里不对。
--
-- 另一处是 NULL 与 '' 的二义性：banners.header / content / url 与 notices.title
-- 可空，但读取端一律 COALESCE(…, '')、写入端一律把 '' 转成 NULL。两个值在业务上
-- 完全同义，却在库里有两种存法，于是「按空串过滤」和「按 NULL 过滤」结果不同，
-- 谁都说不清哪种才对。这次统一成 NOT NULL DEFAULT ''，仓储里对应的 nullableString
-- 一并拿掉 —— 那个转换正是 platform_banners 注释里记着的 23502 陷阱的来源。
--
-- 本文件可重复执行 —— 迁移运行器没有版本表，每次启动都会把所有 up.sql 重跑一遍。

/* ─────────────── 1. 消除 NULL 与 '' 的二义性 ─────────────── */

UPDATE banners SET header  = '' WHERE header  IS NULL;
UPDATE banners SET content = '' WHERE content IS NULL;
UPDATE banners SET url     = '' WHERE url     IS NULL;

ALTER TABLE banners
    ALTER COLUMN header  SET DEFAULT '',
    ALTER COLUMN content SET DEFAULT '',
    ALTER COLUMN url     SET DEFAULT '',
    ALTER COLUMN header  SET NOT NULL,
    ALTER COLUMN content SET NOT NULL,
    ALTER COLUMN url     SET NOT NULL;

UPDATE notices SET title = '' WHERE title IS NULL;

ALTER TABLE notices
    ALTER COLUMN title SET DEFAULT '',
    ALTER COLUMN title SET NOT NULL;

/* ─────────────── 2. 枚举白名单收敛存量值 ─────────────── */

-- 先把越界的存量值收敛掉，否则下面的约束加不上。
-- 落点与 000072 一致：展示位统一落到首页轮播。
UPDATE banners SET type = 'hero'
 WHERE type IS NULL OR type NOT IN ('hero', 'popup', 'splash', 'notice', 'card');

UPDATE notices SET type = 'notice'
 WHERE type IS NULL OR type NOT IN ('notice', 'activity', 'maintenance', 'update', 'security');

UPDATE notices SET level = 'normal'
 WHERE level IS NULL OR level NOT IN ('normal', 'important', 'critical');

-- 越界状态落到草稿而不是已发布：拿不准的内容不该自己跑到用户面前去。
UPDATE notices SET status = 'draft'
 WHERE status IS NULL OR status NOT IN ('draft', 'published', 'archived');

UPDATE platform_banners SET type = 'info'
 WHERE type IS NULL OR type NOT IN ('info', 'notice', 'maintenance', 'release', 'security');

/* ─────────────── 3. 约束落库 ─────────────── */

-- 用 pg_constraint 守卫而不是 DROP + ADD：后者每次启动都会重新全表校验一遍，
-- 并且在 ALTER 期间持有 ACCESS EXCLUSIVE 锁。这里只在缺失时加一次。
--
-- 白名单类约束是 VALID 的（上一段刚把存量值收敛过）；
-- 窗口与标题类约束一律 NOT VALID —— 存量里可能已经躺着倒挂的时间窗或空标题，
-- 那些行本来就不生效，是惰性的坏数据。替管理员猜「这个活动到底该不该长期投放」
-- 比留着它更危险，所以只拦新写入，存量留给管理员在控制台上看见并自行修正。
DO $$
BEGIN
    /* ---- banners ---- */
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'banners'::regclass AND conname = 'ck_banners_type') THEN
        ALTER TABLE banners ADD CONSTRAINT ck_banners_type
            CHECK (type IN ('hero', 'popup', 'splash', 'notice', 'card'));
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'banners'::regclass AND conname = 'ck_banners_window') THEN
        ALTER TABLE banners ADD CONSTRAINT ck_banners_window
            CHECK (start_time IS NULL OR end_time IS NULL OR end_time > start_time) NOT VALID;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'banners'::regclass AND conname = 'ck_banners_title') THEN
        ALTER TABLE banners ADD CONSTRAINT ck_banners_title
            CHECK (btrim(title) <> '') NOT VALID;
    END IF;

    /* ---- notices ---- */
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'notices'::regclass AND conname = 'ck_notices_type') THEN
        ALTER TABLE notices ADD CONSTRAINT ck_notices_type
            CHECK (type IN ('notice', 'activity', 'maintenance', 'update', 'security'));
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'notices'::regclass AND conname = 'ck_notices_level') THEN
        ALTER TABLE notices ADD CONSTRAINT ck_notices_level
            CHECK (level IN ('normal', 'important', 'critical'));
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'notices'::regclass AND conname = 'ck_notices_status') THEN
        ALTER TABLE notices ADD CONSTRAINT ck_notices_status
            CHECK (status IN ('draft', 'published', 'archived'));
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'notices'::regclass AND conname = 'ck_notices_window') THEN
        ALTER TABLE notices ADD CONSTRAINT ck_notices_window
            CHECK (start_time IS NULL OR end_time IS NULL OR end_time > start_time) NOT VALID;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'notices'::regclass AND conname = 'ck_notices_title') THEN
        ALTER TABLE notices ADD CONSTRAINT ck_notices_title
            CHECK (btrim(title) <> '') NOT VALID;
    END IF;

    /* ---- platform_banners ---- */
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'platform_banners'::regclass AND conname = 'ck_platform_banners_type') THEN
        ALTER TABLE platform_banners ADD CONSTRAINT ck_platform_banners_type
            CHECK (type IN ('info', 'notice', 'maintenance', 'release', 'security'));
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'platform_banners'::regclass AND conname = 'ck_platform_banners_window') THEN
        ALTER TABLE platform_banners ADD CONSTRAINT ck_platform_banners_window
            CHECK (start_time IS NULL OR end_time IS NULL OR end_time > start_time) NOT VALID;
    END IF;

    -- 平台横幅的图片是必填的（service 已经这么判），空图片在总览页上是一块白。
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'platform_banners'::regclass AND conname = 'ck_platform_banners_required') THEN
        ALTER TABLE platform_banners ADD CONSTRAINT ck_platform_banners_required
            CHECK (btrim(title) <> '' AND btrim(image_url) <> '') NOT VALID;
    END IF;
END
$$;
