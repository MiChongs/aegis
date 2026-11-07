-- +migrate Up
-- 流量管理（docs/traffic-management.md）：取代旧的「突发流量爬坡」单一开关，
-- 以内置插件 traffic 的形式运行。
--
--   native_plugins           内置插件的启停状态（插件宿主读写，与 expr/wasm 插件表 plugins 无关）
--   traffic_settings         全局设置，单行（id = 1），revision 为当前生效的修订号
--   traffic_rules            规则表，position 即匹配顺序
--   traffic_revisions        每次发布的完整快照，用于历史对照与回滚
--   traffic_metrics_minute   各实例每分钟的聚合计数（rule_id 为空串表示实例整体）
--   traffic_events           状态变化事件（预热、过载保护、拒绝突增……）
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS native_plugins (
    id VARCHAR(64) PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by BIGINT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS traffic_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    revision BIGINT NOT NULL DEFAULT 0,
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by BIGINT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_traffic_settings_singleton CHECK (id = 1)
);

CREATE TABLE IF NOT EXISTS traffic_rules (
    id VARCHAR(40) PRIMARY KEY,
    position INTEGER NOT NULL,
    name VARCHAR(64) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    spec JSONB NOT NULL,
    created_by BIGINT NULL,
    updated_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_traffic_rules_position ON traffic_rules(position);

CREATE TABLE IF NOT EXISTS traffic_revisions (
    revision BIGINT PRIMARY KEY,
    action VARCHAR(32) NOT NULL,
    note VARCHAR(200) NOT NULL DEFAULT '',
    snapshot JSONB NOT NULL,
    rule_count INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_traffic_revisions_created ON traffic_revisions(created_at DESC);

CREATE TABLE IF NOT EXISTS traffic_metrics_minute (
    bucket TIMESTAMPTZ NOT NULL,
    instance VARCHAR(96) NOT NULL,
    rule_id VARCHAR(40) NOT NULL DEFAULT '',
    arrivals BIGINT NOT NULL DEFAULT 0,
    admitted BIGINT NOT NULL DEFAULT 0,
    queued BIGINT NOT NULL DEFAULT 0,
    bypassed BIGINT NOT NULL DEFAULT 0,
    observed BIGINT NOT NULL DEFAULT 0,
    rejected_rate BIGINT NOT NULL DEFAULT 0,
    rejected_concurrency BIGINT NOT NULL DEFAULT 0,
    rejected_queue BIGINT NOT NULL DEFAULT 0,
    rejected_timeout BIGINT NOT NULL DEFAULT 0,
    rejected_overload BIGINT NOT NULL DEFAULT 0,
    p50_ms REAL NOT NULL DEFAULT 0,
    p99_ms REAL NOT NULL DEFAULT 0,
    max_inflight BIGINT NOT NULL DEFAULT 0,
    peak_rps BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket, instance, rule_id)
);
-- 历史曲线按 (rule_id, bucket) 范围扫描；保留期清理按 bucket
CREATE INDEX IF NOT EXISTS idx_traffic_metrics_rule_bucket ON traffic_metrics_minute(rule_id, bucket);

CREATE TABLE IF NOT EXISTS traffic_events (
    id BIGSERIAL PRIMARY KEY,
    at TIMESTAMPTZ NOT NULL,
    instance VARCHAR(96) NOT NULL,
    kind VARCHAR(40) NOT NULL,
    rule_id VARCHAR(40) NOT NULL DEFAULT '',
    rule_name VARCHAR(64) NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_traffic_events_at ON traffic_events(at DESC);
CREATE INDEX IF NOT EXISTS idx_traffic_events_kind_at ON traffic_events(kind, at DESC);
