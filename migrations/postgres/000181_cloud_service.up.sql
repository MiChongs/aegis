-- +migrate Up
-- 云服务（docs/cloud-service.md）：云存储升级为「按会员档位分级的存储 + 记录级实时同步 + 设备中心」。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

-- ── 档位：按会员档位区分的空间与同步服务，按优先级排列的 JSON 数组（见 cloudstorage.Plan） ──
ALTER TABLE app_cloud_storage_configs ADD COLUMN IF NOT EXISTS plans JSONB NOT NULL DEFAULT '[]'::jsonb;

-- ── 用户账目：同步记录的用量与按用户单调递增的序号 ──
-- sync_seq 是这个用户最后一次被接受的写入拿到的序号（增量拉取的游标空间）；
-- sync_floor_seq 是已经清掉的墓碑里最大的序号：比它旧的游标要全量重拉。
ALTER TABLE app_cloud_storage_users ADD COLUMN IF NOT EXISTS sync_seq BIGINT NOT NULL DEFAULT 0;
ALTER TABLE app_cloud_storage_users ADD COLUMN IF NOT EXISTS sync_floor_seq BIGINT NOT NULL DEFAULT 0;
ALTER TABLE app_cloud_storage_users ADD COLUMN IF NOT EXISTS sync_bytes BIGINT NOT NULL DEFAULT 0;
ALTER TABLE app_cloud_storage_users ADD COLUMN IF NOT EXISTS sync_records INTEGER NOT NULL DEFAULT 0;
ALTER TABLE app_cloud_storage_users ADD COLUMN IF NOT EXISTS last_sync_at TIMESTAMPTZ NULL;

-- ── 同步记录：（用户, 集合, 记录 ID）一行，逐条最后写入者胜 ──
-- 主键以 user_id 打头：删用户时级联删除走的就是这个索引（新外键必须带索引）。
CREATE TABLE IF NOT EXISTS app_cloud_sync_records (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    collection VARCHAR(64) NOT NULL,
    record_id VARCHAR(200) NOT NULL,
    -- 墓碑（deleted）时为 NULL。
    payload JSONB NULL,
    size INTEGER NOT NULL DEFAULT 0,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    -- 客户端修改时间：最后写入者胜的依据（同一毫秒按 device_id 定胜负）。
    client_updated_at TIMESTAMPTZ NOT NULL,
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    seq BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, appid, collection, record_id)
);

-- 增量拉取：按序号顺序扫。
CREATE INDEX IF NOT EXISTS idx_app_cloud_sync_records_seq
    ON app_cloud_sync_records(user_id, appid, seq);
-- 浏览：按集合、按修改时间倒序。
CREATE INDEX IF NOT EXISTS idx_app_cloud_sync_records_browse
    ON app_cloud_sync_records(user_id, appid, collection, client_updated_at DESC);
-- 应用级统计与级联删除应用。
CREATE INDEX IF NOT EXISTS idx_app_cloud_sync_records_appid
    ON app_cloud_sync_records(appid, collection);
-- 墓碑清理只扫墓碑。
CREATE INDEX IF NOT EXISTS idx_app_cloud_sync_records_tombstones
    ON app_cloud_sync_records(appid, updated_at) WHERE deleted;

-- ── 同步设备：一台登记过的设备一行（档位限制设备数） ──
CREATE TABLE IF NOT EXISTS app_cloud_sync_devices (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    device_id VARCHAR(128) NOT NULL,
    name VARCHAR(64) NOT NULL DEFAULT '',
    -- 用户改过名字后不再被会话里的型号名覆盖。
    custom_name BOOLEAN NOT NULL DEFAULT FALSE,
    platform VARCHAR(32) NOT NULL DEFAULT '',
    model VARCHAR(64) NOT NULL DEFAULT '',
    os VARCHAR(32) NOT NULL DEFAULT '',
    os_version VARCHAR(32) NOT NULL DEFAULT '',
    app_version VARCHAR(32) NOT NULL DEFAULT '',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_sync_at TIMESTAMPTZ NULL,
    last_seq BIGINT NOT NULL DEFAULT 0,
    -- 正在播放 / 阅读：{collection, recordId, state, payload, at}
    activity JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, appid, device_id)
);

CREATE INDEX IF NOT EXISTS idx_app_cloud_sync_devices_appid
    ON app_cloud_sync_devices(appid, last_seen_at DESC);

-- ── 接力：把一条记录发到另一台设备上继续（档位开关 handoff） ──
CREATE TABLE IF NOT EXISTS app_cloud_sync_handoffs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    from_device_id VARCHAR(128) NOT NULL,
    to_device_id VARCHAR(128) NOT NULL,
    collection VARCHAR(64) NOT NULL,
    record_id VARCHAR(200) NOT NULL,
    payload JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    acked_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_app_cloud_sync_handoffs_user
    ON app_cloud_sync_handoffs(user_id, appid, to_device_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_cloud_sync_handoffs_appid
    ON app_cloud_sync_handoffs(appid, expires_at);
