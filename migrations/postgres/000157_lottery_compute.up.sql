-- +migrate Up
-- 抽奖的大规模推演任务（docs/lottery.md「计算引擎」）。
-- 推演是纯计算、不改任何业务数据，但一次可以跑几千万次抽奖、几十秒：做成落库的任务，
-- 发起的请求立刻返回，任意一个实例认领后执行，进度与结果写回这张表；执行它的实例中途没了，
-- 心跳过期后由别的实例（或重启后的自己）按同一个种子从头重跑，得到同一份结果。
-- 本文件随每次启动整份重放，全部语句必须幂等。

CREATE TABLE IF NOT EXISTS lottery_compute_jobs (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 推演的活动；推演未保存的新活动时为空。活动删了任务照样留着（结果里自带奖品名）
    activity_id BIGINT NULL,
    activity_name VARCHAR(128) NOT NULL DEFAULT '',
    name VARCHAR(64) NOT NULL DEFAULT '',
    -- queued 排队 / running 执行中 / succeeded 完成 / failed 失败 / cancelled 已取消
    status VARCHAR(16) NOT NULL DEFAULT 'queued',
    -- lottery.ComputeScenario：解析好的完整场景（各人群眼里的活动与奖盘、规模、种子、起始日）。
    -- 存解析后的而不是原始请求：重跑时不再读活动与会员配置，改了配置也不影响已经发起的任务
    scenario JSONB NOT NULL,
    -- 规模摘要（人数、天数、次数、平行世界数、总抽奖次数、人群），列表上用，不必解场景
    summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- lottery.ComputeResult，完成后写入
    result JSONB NULL,
    -- 已合并 / 总共的平行世界数
    progress INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT '',
    -- 执行它的实例（集群节点标识）与心跳；心跳过期即视为实例已经没了
    owner VARCHAR(128) NOT NULL DEFAULT '',
    heartbeat_at TIMESTAMPTZ NULL,
    -- 被认领过几次：反复把实例跑垮的任务到次数后判失败，不再接手
    attempts INTEGER NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    created_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_lottery_compute_jobs_app ON lottery_compute_jobs(appid, id DESC);
-- 认领只看排队与执行中的，这类任务同一时刻只有个位数
CREATE INDEX IF NOT EXISTS idx_lottery_compute_jobs_pending ON lottery_compute_jobs(id) WHERE status IN ('queued', 'running');

-- 场景与结果是几 KB 到几十 KB 的 JSON，读列表时不取：用 lz4 压缩（与 000149 给抽奖记录做的一样，只做一次）
DO $$
BEGIN
    IF (SELECT attcompression FROM pg_attribute
        WHERE attrelid = 'lottery_compute_jobs'::regclass AND attname = 'result') IS DISTINCT FROM 'l' THEN
        ALTER TABLE lottery_compute_jobs ALTER COLUMN result SET COMPRESSION lz4;
        ALTER TABLE lottery_compute_jobs ALTER COLUMN scenario SET COMPRESSION lz4;
    END IF;
EXCEPTION WHEN feature_not_supported OR invalid_parameter_value THEN
    -- 没编进 lz4 的 Postgres：保持默认的 pglz
    NULL;
END $$;
