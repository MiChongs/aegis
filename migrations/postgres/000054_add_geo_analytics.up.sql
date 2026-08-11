-- 地理风控 & 分析能力（依赖 PostGIS，镜像见 deploy/docker/postgres/Dockerfile）
--
-- 架构分层（详见 internal/service/geo_*.go）：
--   L1 请求热路径：mmdb + 内存规则判定，不触达本迁移中的任何表
--   L2 近线风控  ：Worker 消费登录事件 → login_geo_events / user_geo_profiles
--   L3 离线分析  ：geo_stats_hourly 预聚合 + PostGIS 空间查询（聚类/回测/轨迹）

CREATE EXTENSION IF NOT EXISTS postgis;

-- ──────────────────────────────────────
-- ① firewall_logs：经纬度物化为空间点
--    生成列自动跟随 Worker 的 GeoIP 回填，存量行一并生效
-- ──────────────────────────────────────

ALTER TABLE firewall_logs ADD COLUMN IF NOT EXISTS geom geography(Point, 4326)
    GENERATED ALWAYS AS (
        CASE WHEN latitude IS NOT NULL AND longitude IS NOT NULL
                  AND latitude BETWEEN -90 AND 90 AND longitude BETWEEN -180 AND 180
             THEN ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography
        END
    ) STORED;
CREATE INDEX IF NOT EXISTS idx_fw_logs_geom ON firewall_logs USING GIST (geom);

-- ──────────────────────────────────────
-- ② 登录地理事件（风控明细，按月分区；分区由函数管理）
-- ──────────────────────────────────────

CREATE TABLE IF NOT EXISTS login_geo_events (
    id           BIGINT GENERATED ALWAYS AS IDENTITY,
    user_id      BIGINT NOT NULL,
    app_id       BIGINT NOT NULL,
    ip           INET   NOT NULL,
    country_code VARCHAR(8)   NOT NULL DEFAULT '',
    country      VARCHAR(64)  NOT NULL DEFAULT '',
    region       VARCHAR(128) NOT NULL DEFAULT '',
    city         VARCHAR(128) NOT NULL DEFAULT '',
    asn          VARCHAR(16)  NOT NULL DEFAULT '',
    isp          VARCHAR(128) NOT NULL DEFAULT '',
    geom         geography(Point, 4326),
    login_type   VARCHAR(32)  NOT NULL DEFAULT '',
    device_id    VARCHAR(191) NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE INDEX IF NOT EXISTS idx_lge_user_time ON login_geo_events (user_id, app_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_lge_geom ON login_geo_events USING GIST (geom);
CREATE INDEX IF NOT EXISTS idx_lge_country_time ON login_geo_events (country_code, created_at DESC);

-- 分区管理函数：确保 month_start 所在月份的分区存在（Worker 定时调用，迁移时初始化两个月）
CREATE OR REPLACE FUNCTION ensure_login_geo_partition(month_start DATE) RETURNS void AS $$
DECLARE
    part_start DATE := date_trunc('month', month_start)::date;
    part_end   DATE := (date_trunc('month', month_start) + INTERVAL '1 month')::date;
    part_name  TEXT := 'login_geo_events_' || to_char(part_start, 'YYYYMM');
BEGIN
    IF to_regclass(part_name) IS NULL THEN
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF login_geo_events FOR VALUES FROM (%L) TO (%L)',
            part_name, part_start, part_end
        );
    END IF;
END;
$$ LANGUAGE plpgsql;

SELECT ensure_login_geo_partition(NOW()::date);
SELECT ensure_login_geo_partition((NOW() + INTERVAL '1 month')::date);

-- ──────────────────────────────────────
-- ③ 地理围栏规则（真实来源；运行时判定在 Go 内存进行）
-- ──────────────────────────────────────

CREATE TABLE IF NOT EXISTS geo_fences (
    id          BIGSERIAL PRIMARY KEY,
    app_id      BIGINT NULL,                              -- NULL = 平台级
    name        VARCHAR(128) NOT NULL,
    mode        VARCHAR(16) NOT NULL DEFAULT 'deny',      -- deny | allow | review
    fence       geography(MultiPolygon, 4326) NULL,       -- 多边形围栏
    center      geography(Point, 4326) NULL,              -- 圆形围栏圆心
    radius_m    DOUBLE PRECISION NULL,                    -- 圆形围栏半径（米）
    ban_mode    VARCHAR(32) NOT NULL DEFAULT '',          -- 命中后的响应模式；空 = 平台默认
    reason      VARCHAR(255) NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at  TIMESTAMPTZ NULL,
    match_count BIGINT NOT NULL DEFAULT 0,
    last_match_at TIMESTAMPTZ NULL,
    created_by  BIGINT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT geo_fences_shape_check CHECK (
        fence IS NOT NULL OR (center IS NOT NULL AND radius_m IS NOT NULL AND radius_m > 0)
    )
);
CREATE INDEX IF NOT EXISTS idx_geo_fences_enabled ON geo_fences (enabled) WHERE enabled = TRUE;
CREATE INDEX IF NOT EXISTS idx_geo_fences_gist ON geo_fences USING GIST (fence);

COMMENT ON TABLE geo_fences IS '地理围栏规则：deny=区域内拦截 / allow=区域外拦截（存在任一 allow 围栏时生效）/ review=仅记录';

-- ──────────────────────────────────────
-- ④ 用户地理画像（近线风控热数据；Redis 缓存读主，本表持久化）
-- ──────────────────────────────────────

CREATE TABLE IF NOT EXISTS user_geo_profiles (
    user_id         BIGINT NOT NULL,
    app_id          BIGINT NOT NULL,
    home_geom       geography(Point, 4326) NULL,   -- 90 天登录质心（每日重算）
    home_radius_m   DOUBLE PRECISION NOT NULL DEFAULT 0,  -- 质心到登录点的 P90 距离
    known_countries TEXT[] NOT NULL DEFAULT '{}',
    last_geom       geography(Point, 4326) NULL,
    last_country    VARCHAR(8) NOT NULL DEFAULT '',
    last_ip         INET NULL,
    last_login_at   TIMESTAMPTZ NULL,
    login_count     BIGINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, app_id)
);

-- ──────────────────────────────────────
-- ⑤ 地理统计小时汇总（仪表盘唯一数据源，避免扫明细表）
-- ──────────────────────────────────────

CREATE TABLE IF NOT EXISTS geo_stats_hourly (
    bucket       TIMESTAMPTZ NOT NULL,
    kind         VARCHAR(16) NOT NULL,            -- block | login
    country_code VARCHAR(8)  NOT NULL DEFAULT '',
    city         VARCHAR(128) NOT NULL DEFAULT '',
    geohash5     VARCHAR(8)  NOT NULL DEFAULT '', -- ST_GeoHash 精度 5（约 5km 网格）
    lat          DOUBLE PRECISION NOT NULL DEFAULT 0,  -- 网格代表点（质心），前端直接渲染
    lng          DOUBLE PRECISION NOT NULL DEFAULT 0,
    cnt          BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket, kind, country_code, city, geohash5)
);
CREATE INDEX IF NOT EXISTS idx_geo_stats_kind_bucket ON geo_stats_hourly (kind, bucket DESC);
