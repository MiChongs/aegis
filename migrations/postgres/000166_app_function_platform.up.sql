-- 远程函数平台化：触发器（平台事件 / 定时）、持久触发队列、调用记录留存入参与日志、权益发放幂等账。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

-- ── 调用记录 ──
-- source：这次调用从哪来。接入方调用（api）、事件触发（event）、定时触发（schedule）、
-- 控制台真实调用（console）、函数互调（function）。排障时第一个问题就是「谁拉起的它」。
ALTER TABLE app_function_invocations ADD COLUMN IF NOT EXISTS source VARCHAR(16) NOT NULL DEFAULT 'api';
ALTER TABLE app_function_invocations ADD COLUMN IF NOT EXISTS trigger_id BIGINT NULL;
ALTER TABLE app_function_invocations ADD COLUMN IF NOT EXISTS error_code INTEGER NOT NULL DEFAULT 0;
-- 入参与日志：只有试跑回传日志的话，线上那条失败调用为什么失败只能去翻服务器日志；
-- 留入参是为了「用这次的入参与身份再试跑一遍」。入参超过 16KB 的不留（input_truncated）。
ALTER TABLE app_function_invocations ADD COLUMN IF NOT EXISTS input JSONB NULL;
ALTER TABLE app_function_invocations ADD COLUMN IF NOT EXISTS input_truncated BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE app_function_invocations ADD COLUMN IF NOT EXISTS logs JSONB NULL;
ALTER TABLE app_function_invocations ADD COLUMN IF NOT EXISTS usage JSONB NULL;

CREATE INDEX IF NOT EXISTS idx_app_function_invocations_app_time
    ON app_function_invocations(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_function_invocations_trigger
    ON app_function_invocations(appid, trigger_id, created_at DESC) WHERE trigger_id IS NOT NULL;

-- ── 触发器 ──
CREATE TABLE IF NOT EXISTS app_function_triggers (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    function_id BIGINT NOT NULL REFERENCES app_functions(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('event', 'schedule')),
    event VARCHAR(64) NOT NULL DEFAULT '',
    cron VARCHAR(64) NOT NULL DEFAULT '',
    timezone VARCHAR(64) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at TIMESTAMPTZ NULL,
    last_fire_at TIMESTAMPTZ NULL,
    last_status VARCHAR(16) NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    fire_count BIGINT NOT NULL DEFAULT 0,
    fail_count BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((kind = 'event' AND event <> '') OR (kind = 'schedule' AND cron <> ''))
);

CREATE INDEX IF NOT EXISTS idx_app_function_triggers_function ON app_function_triggers(appid, function_id);
-- 钩子回调按 (应用, 事件) 找启用中的触发器
CREATE INDEX IF NOT EXISTS idx_app_function_triggers_event
    ON app_function_triggers(appid, event) WHERE enabled AND kind = 'event';
-- 定时扫描按到点时间找
CREATE INDEX IF NOT EXISTS idx_app_function_triggers_due
    ON app_function_triggers(next_run_at) WHERE enabled AND kind = 'schedule';

-- ── 持久触发队列 ──
-- 钩子回调与定时扫描只入队；执行由后台循环 FOR UPDATE SKIP LOCKED 抢占，
-- 失败按退避重试。event_id 就是这次函数调用的 eventId，重试时函数侧的幂等照样生效。
CREATE TABLE IF NOT EXISTS app_function_trigger_jobs (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    function_id BIGINT NOT NULL REFERENCES app_functions(id) ON DELETE CASCADE,
    trigger_id BIGINT NOT NULL REFERENCES app_function_triggers(id) ON DELETE CASCADE,
    event_id UUID NOT NULL,
    kind VARCHAR(16) NOT NULL,
    event VARCHAR(64) NOT NULL DEFAULT '',
    user_id BIGINT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- dedupe_key 同一个触发器内的幂等键：同一个事件（同一笔订单、同一次注册）只入队一次
    dedupe_key VARCHAR(160) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_until TIMESTAMPTZ NULL,
    fired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ NULL,
    UNIQUE (trigger_id, dedupe_key)
);

CREATE INDEX IF NOT EXISTS idx_app_function_trigger_jobs_due
    ON app_function_trigger_jobs(run_after) WHERE status IN ('pending', 'running');
CREATE INDEX IF NOT EXISTS idx_app_function_trigger_jobs_trigger
    ON app_function_trigger_jobs(trigger_id, fired_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_function_trigger_jobs_finished
    ON app_function_trigger_jobs(finished_at) WHERE finished_at IS NOT NULL;

-- ── 权益发放幂等账 ──
-- aegis.rights.grant 发出去的每一笔都记在这里：(应用, 函数, 用户, 幂等键) 唯一，
-- 同一个键第二次发放直接回第一次的结果 —— 脚本重试、eventId 重放都不会多发。
CREATE TABLE IF NOT EXISTS app_function_grants (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    function_id BIGINT NOT NULL REFERENCES app_functions(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    event_id UUID NULL,
    grant_key VARCHAR(160) NOT NULL,
    title VARCHAR(128) NOT NULL DEFAULT '',
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (appid, function_id, user_id, grant_key)
);

CREATE INDEX IF NOT EXISTS idx_app_function_grants_user ON app_function_grants(appid, user_id, created_at DESC);
