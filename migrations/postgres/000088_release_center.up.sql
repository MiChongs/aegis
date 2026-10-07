-- +migrate Up
-- 发布中心：把 app_versions 从「一条下载地址 + 一个强更开关」升级成可定向、可灰度、
-- 可定时、带多安装包与漏斗统计的发布对象。
--
-- 升级前的三个结构性缺口：
--   1. 渠道表上的 rollout_pct / rules / platforms 只落库、不参与检测，灰度是摆设
--   2. 一个版本只有一个 download_url，而 Android 按 ABI 分包，一次发布是四五个安装包
--   3. 没有任何下发、下载、安装的统计，放量到多少、装上了多少全凭猜
--
-- 新增列全部带默认值，存量行落在「公开 / 全量 / 已发布 / 可选更新」，行为与升级前一致。
-- 本文件可重复执行 —— 迁移运行器每次启动都会把所有 up.sql 重跑一遍。

ALTER TABLE app_versions
    ADD COLUMN IF NOT EXISTS title              VARCHAR(255) NOT NULL DEFAULT '',
    -- release_notes 存净化后的富文本 HTML；notes_summary 是服务端提取的纯文本摘要
    ADD COLUMN IF NOT EXISTS notes_summary      TEXT         NOT NULL DEFAULT '',
    -- public：任何人（含未登录与官网）；signed_in：仅登录用户；testers：仅内测名单
    ADD COLUMN IF NOT EXISTS visibility         VARCHAR(16)  NOT NULL DEFAULT 'public',
    ADD COLUMN IF NOT EXISTS targeting          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS rollout_pct        INT          NOT NULL DEFAULT 100,
    -- 低于此版本码的客户端必须更新（0 = 不限）
    ADD COLUMN IF NOT EXISTS min_supported_code BIGINT       NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS publish_at         TIMESTAMPTZ  NULL,
    ADD COLUMN IF NOT EXISTS published_at       TIMESTAMPTZ  NULL,
    ADD COLUMN IF NOT EXISTS created_by         BIGINT       NULL;

-- 存量：强更开关并入 update_type；已发布的补发布时间，否则失去排序依据
UPDATE app_versions SET update_type = 'force' WHERE force_update = true AND update_type <> 'force';
UPDATE app_versions SET update_type = 'optional'
 WHERE update_type NOT IN ('optional', 'recommended', 'force');
UPDATE app_versions SET published_at = created_at WHERE published_at IS NULL AND status = 'published';
UPDATE app_versions SET status = 'draft'
 WHERE status NOT IN ('draft', 'scheduled', 'published', 'paused', 'revoked');

-- 渠道：允许用户自助加入（如 Beta 体验计划）
ALTER TABLE app_version_channels
    ADD COLUMN IF NOT EXISTS self_join BOOLEAN NOT NULL DEFAULT false;

-- 安装包：一个版本按 ABI（或任意变体）挂多个
CREATE TABLE IF NOT EXISTS app_version_assets (
    id             BIGSERIAL PRIMARY KEY,
    version_id     BIGINT       NOT NULL REFERENCES app_versions(id) ON DELETE CASCADE,
    appid          BIGINT       NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    abi            VARCHAR(32)  NOT NULL DEFAULT 'universal',
    label          VARCHAR(128) NOT NULL DEFAULT '',
    -- 外链地址或 storage://{configId}/{objectKey} 引用（读取时现解析成可访问地址）
    url            TEXT         NOT NULL,
    file_size      BIGINT       NOT NULL DEFAULT 0,
    sha256         VARCHAR(64)  NOT NULL DEFAULT '',
    position       INT          NOT NULL DEFAULT 0,
    download_count BIGINT       NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_app_version_assets_version ON app_version_assets(version_id, position, id);

-- 存量的单一下载地址迁成一个通用包
INSERT INTO app_version_assets (version_id, appid, abi, label, url, file_size, sha256)
SELECT v.id, v.appid, 'universal', '', v.download_url, v.file_size,
       CASE WHEN v.file_hash ~ '^[0-9a-fA-F]{64}$' THEN lower(v.file_hash) ELSE '' END
  FROM app_versions v
 WHERE btrim(v.download_url) <> ''
   AND NOT EXISTS (SELECT 1 FROM app_version_assets a WHERE a.version_id = v.id);

-- 漏斗统计：按天聚合，一行一版本一天
CREATE TABLE IF NOT EXISTS app_version_daily_stats (
    version_id BIGINT NOT NULL REFERENCES app_versions(id) ON DELETE CASCADE,
    day        DATE   NOT NULL,
    offered    BIGINT NOT NULL DEFAULT 0,
    downloaded BIGINT NOT NULL DEFAULT 0,
    installed  BIGINT NOT NULL DEFAULT 0,
    failed     BIGINT NOT NULL DEFAULT 0,
    dismissed  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (version_id, day)
);

-- 检测接口的主查询：某应用下可下发的版本
CREATE INDEX IF NOT EXISTS idx_app_versions_release_active
    ON app_versions(appid, version_code DESC)
    WHERE status IN ('published', 'scheduled');

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                    WHERE conrelid = 'app_versions'::regclass AND conname = 'ck_app_versions_release') THEN
        ALTER TABLE app_versions ADD CONSTRAINT ck_app_versions_release CHECK (
            status IN ('draft', 'scheduled', 'published', 'paused', 'revoked')
            AND visibility IN ('public', 'signed_in', 'testers')
            AND update_type IN ('optional', 'recommended', 'force')
            AND rollout_pct BETWEEN 0 AND 100
        );
    END IF;
END
$$;
