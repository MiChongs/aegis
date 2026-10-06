-- +migrate Up
-- 应用级用户云存储：每个用户在应用内的一块私有空间，按「命名空间 / 键」存放任意文档
-- （收藏、设置、草稿、存档……），每次写入留一个修订版本，可回滚、可进回收站。
--
-- 内容本身**不进数据库**：字节交给应用解析出的存储配置（S3 / OSS / COS / MinIO /
-- WebDAV / 本地……，与 /storage/upload 同一套 resolveConfig），这里只存索引与配额账目。
-- 这样换存储提供商不需要迁移数据库，大文档也不会把 Postgres 撑大。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

-- ── 配置：一个应用一行 ──
CREATE TABLE IF NOT EXISTS app_cloud_storage_configs (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- 内容写到哪个存储配置。空串表示走应用的默认解析（应用级默认 → 平台级默认），
    -- 与 /storage/upload 不带 config_name 时完全一致。
    storage_config_name VARCHAR(128) NOT NULL DEFAULT '',
    -- 每个用户的默认配额（字节）。计入的是**全部留存修订**，含回收站里的条目 ——
    -- 它们在被清除之前确实占着存储桶。
    quota_bytes BIGINT NOT NULL DEFAULT 20971520,
    max_item_bytes BIGINT NOT NULL DEFAULT 1048576,
    max_items INTEGER NOT NULL DEFAULT 500,
    -- 每个条目保留的修订数（含当前版本），超出的最旧修订在写入时同事务裁掉。
    max_revisions INTEGER NOT NULL DEFAULT 10,
    trash_retention_days INTEGER NOT NULL DEFAULT 30,
    -- 命名空间目录：[{key, name, description}]。restrict_namespaces 为真时只允许目录内的键，
    -- 否则目录只用于控制台展示名称。
    restrict_namespaces BOOLEAN NOT NULL DEFAULT FALSE,
    namespaces JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── 用户：配额覆盖、冻结与用量账目 ──
--
-- 用量列是**从修订表重算**出来的快照（每次写入在同一事务里 SELECT SUM 再写回），
-- 不是逐次加减的计数器：加减式计数器一旦漏掉一个分支就永久漂移，而重算的代价只是
-- 单个用户的几百行。保留这几列是为了让管理端能按用量排序而不必全表聚合。
CREATE TABLE IF NOT EXISTS app_cloud_storage_users (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- NULL 表示沿用应用默认配额。
    quota_bytes BIGINT NULL,
    -- 冻结后只读：读取、下载照常，写入 / 删除 / 回滚一律 40361。
    frozen BOOLEAN NOT NULL DEFAULT FALSE,
    frozen_reason VARCHAR(255) NOT NULL DEFAULT '',
    note VARCHAR(255) NOT NULL DEFAULT '',
    used_bytes BIGINT NOT NULL DEFAULT 0,
    item_count INTEGER NOT NULL DEFAULT 0,
    trash_count INTEGER NOT NULL DEFAULT 0,
    revision_count INTEGER NOT NULL DEFAULT 0,
    last_write_at TIMESTAMPTZ NULL,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id)
);

CREATE INDEX IF NOT EXISTS idx_app_cloud_storage_users_usage
    ON app_cloud_storage_users(appid, used_bytes DESC);

-- ── 条目：一个（用户, 命名空间, 键）一行，指向当前修订 ──
CREATE TABLE IF NOT EXISTS app_cloud_items (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    namespace VARCHAR(64) NOT NULL,
    item_key VARCHAR(128) NOT NULL,
    -- 单调递增的修订号：乐观并发的依据（ifRevision）。进回收站再恢复不重置。
    revision BIGINT NOT NULL DEFAULT 0,
    content_type VARCHAR(128) NOT NULL DEFAULT 'application/json',
    -- json / text / base64：客户端写入时的编码，读取时按同一种编码交回。
    encoding VARCHAR(16) NOT NULL DEFAULT 'json',
    size BIGINT NOT NULL DEFAULT 0,
    sha256 CHAR(64) NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    -- 非空即在回收站：不出现在默认列表里，过了保留期由后台循环连同修订一起清除。
    deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_app_cloud_items_key UNIQUE (appid, user_id, namespace, item_key)
);

CREATE INDEX IF NOT EXISTS idx_app_cloud_items_user
    ON app_cloud_items(appid, user_id, namespace, updated_at DESC);
-- 后台清理只扫回收站里的行。
CREATE INDEX IF NOT EXISTS idx_app_cloud_items_trash
    ON app_cloud_items(appid, deleted_at)
    WHERE deleted_at IS NOT NULL;

-- ── 修订：每次写入一行，每行对应存储桶里的一个对象 ──
CREATE TABLE IF NOT EXISTS app_cloud_item_revisions (
    id BIGSERIAL PRIMARY KEY,
    item_id BIGINT NOT NULL REFERENCES app_cloud_items(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    revision BIGINT NOT NULL,
    -- 写入时实际用到的存储配置。后来管理员换了配置，旧修订照样能从原处读出来。
    storage_config_id BIGINT NOT NULL,
    object_key TEXT NOT NULL,
    content_type VARCHAR(128) NOT NULL DEFAULT 'application/json',
    encoding VARCHAR(16) NOT NULL DEFAULT 'json',
    size BIGINT NOT NULL DEFAULT 0,
    sha256 CHAR(64) NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    -- write 用户写入 / upload 文件上传 / rollback 回滚生成 / admin 管理端代写
    source VARCHAR(16) NOT NULL DEFAULT 'write',
    restored_from BIGINT NULL,
    -- 管理端操作留操作人账号；用户自己写入时为空。
    operator VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_app_cloud_item_revisions UNIQUE (item_id, revision)
);

CREATE INDEX IF NOT EXISTS idx_app_cloud_item_revisions_user
    ON app_cloud_item_revisions(appid, user_id);
-- 写入趋势（管理端概览）按应用与时间扫。
CREATE INDEX IF NOT EXISTS idx_app_cloud_item_revisions_time
    ON app_cloud_item_revisions(appid, created_at DESC);
