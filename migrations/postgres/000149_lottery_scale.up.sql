-- +migrate Up
-- 抽奖的高并发改造（docs/lottery.md「容量与并发」）。
--
-- 2026-11-08 01:20–02:45 线上锁冲突的根因：抽奖事务在锁住用户行之后，逐次扫 lottery_draws 求
-- 「本活动今天抽了几次 / 发了多少积分 / 同设备同 IP 几个账号 / 这个人每个奖品中过几次」。
-- 这些是 O(当天全部记录) 与 O(这个人全部记录) 的聚合：一个持有 5 万次卡密次数的人连抽时，
-- 每一次都比上一次慢，同一个人的其余请求排在用户行锁后面，10 条连接的池被占满，整站超时。
--
-- 改成计数：
--   lottery_user_states    这个人在这个活动里的全部计数（今天的免费 / 积分 / 抽奖 / 中奖次数、每个奖品
--                           中过几次、最近抽中的奖品、近 10 分钟抽了几次），抽奖时随状态行一起读写；
--   lottery_activity_deltas 每次抽奖在事务里追加一行增量（只插入，没有全站共享的行要锁），
--   lottery_activity_daily  后台把增量原子地并进每天的全站计数（抽了几次、中了几次、发了多少积分）；
--   lottery_prize_stock     限量奖品的库存与每日限量拆成最多 16 个分片行，事务里挑一个有余量的扣（不再是全站一行）；
--   lottery_risk_keys       本活动当天每台设备 / 每个 IP / 每个网段上抽过奖的账号（风控聚集信号）。
-- 抽奖事务里不再有任何对 lottery_draws 的聚合查询，也不再有全站一行的热点；所有计数都随抽奖事务提交，不会丢。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行；锁表的改动只在需要时做一次。

-- ── 本人计数 ──
-- counted_draws：计数里已经算进去的抽奖次数。与 total_draws 不等（新加的列是 -1、滚动发布期间旧版本
-- 抽过）时，下一次抽奖在锁内按记录重算这个人的计数，之后两者恒等。
ALTER TABLE lottery_user_states
    ADD COLUMN IF NOT EXISTS counted_draws BIGINT NOT NULL DEFAULT -1,
    ADD COLUMN IF NOT EXISTS day_start TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS free_today INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS points_today INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS draws_today INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS wins_today INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS wins_total BIGINT NOT NULL DEFAULT 0,
    -- {"奖品 ID": 中过几次}；只记中奖（谢谢参与不记）
    ADD COLUMN IF NOT EXISTS prize_wins JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS prize_wins_today JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- 最近抽中的奖品 ID，新的在前，未中记 0，最多 100 个（中奖冷却用）
    ADD COLUMN IF NOT EXISTS recent BIGINT[] NOT NULL DEFAULT '{}',
    -- 近 10 分钟抽了几次：按 10 分钟对齐的窗口计数，读时按上一窗口的剩余比例折算（滑动窗口）
    ADD COLUMN IF NOT EXISTS burst_start TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS burst_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS burst_prev INTEGER NOT NULL DEFAULT 0;

-- ── 本活动每天的全站计数 ──
-- shard 0 是后台合并增量写进来的；-1 是首次启动时从历史记录补出来的那一份。
CREATE TABLE IF NOT EXISTS lottery_activity_daily (
    activity_id BIGINT NOT NULL REFERENCES lottery_activities(id) ON DELETE CASCADE,
    day_start TIMESTAMPTZ NOT NULL,
    shard SMALLINT NOT NULL,
    draws BIGINT NOT NULL DEFAULT 0,
    wins BIGINT NOT NULL DEFAULT 0,
    points_issued BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (activity_id, day_start, shard)
);

-- 抽奖事务里追加的增量。后台合并（Repository.CompactLotteryActivityDeltas）在一条语句里删掉并加进
-- lottery_activity_daily，原子；读全站计数时 = 日计数 + 还没合并的增量。
CREATE TABLE IF NOT EXISTS lottery_activity_deltas (
    id BIGSERIAL PRIMARY KEY,
    activity_id BIGINT NOT NULL REFERENCES lottery_activities(id) ON DELETE CASCADE,
    day_start TIMESTAMPTZ NOT NULL,
    draws INTEGER NOT NULL DEFAULT 1,
    wins INTEGER NOT NULL DEFAULT 0,
    points_issued BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_lottery_activity_deltas_day ON lottery_activity_deltas(activity_id, day_start);
ALTER TABLE lottery_activity_deltas SET (autovacuum_vacuum_scale_factor = 0, autovacuum_vacuum_threshold = 5000,
    autovacuum_analyze_scale_factor = 0, autovacuum_analyze_threshold = 5000);

-- ── 限量奖品的库存分片 ──
-- scope 0 是总库存（day_start 固定为 1970-01-01）、1 是某一天的每日限量。每个分片有自己的上限 cap 与已出 used，
-- 各分片之和就是奖品的总上限与已出。抽奖时在事务里随机挑一个有余量、没被别人锁着的分片 +1（FOR UPDATE SKIP LOCKED），
-- 挑不到时再等锁；回滚即还原，不需要补偿。改库存 / 每日限量时在活动的独占咨询锁下重新分配（此时没有进行中的抽奖）。
CREATE TABLE IF NOT EXISTS lottery_prize_stock (
    prize_id BIGINT NOT NULL REFERENCES lottery_prizes(id) ON DELETE CASCADE,
    scope SMALLINT NOT NULL,
    day_start TIMESTAMPTZ NOT NULL,
    bucket SMALLINT NOT NULL,
    cap INTEGER NOT NULL,
    used INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (prize_id, scope, day_start, bucket),
    CONSTRAINT ck_lottery_prize_stock_used CHECK (used >= 0 AND used <= cap)
);
CREATE INDEX IF NOT EXISTS idx_lottery_prize_stock_day ON lottery_prize_stock(scope, day_start);

-- 已有的限量奖品：按现有的 quantity / used 建出总库存分片（已出的全记在 0 号分片，余量平均分）。
-- 只给还没有分片的奖品建，可重复执行。
INSERT INTO lottery_prize_stock (prize_id, scope, day_start, bucket, cap, used)
SELECT p.id, 0, 'epoch'::timestamptz, b.bucket,
       (GREATEST(p.quantity - p.used, 0) / b.n) + CASE WHEN b.bucket < GREATEST(p.quantity - p.used, 0) % b.n THEN 1 ELSE 0 END
         + CASE WHEN b.bucket = 0 THEN LEAST(p.used, p.quantity) ELSE 0 END,
       CASE WHEN b.bucket = 0 THEN LEAST(p.used, p.quantity) ELSE 0 END
FROM lottery_prizes p
CROSS JOIN LATERAL (
    SELECT g AS bucket, LEAST(16, GREATEST(p.quantity - p.used, 1))::int AS n
    FROM generate_series(0, LEAST(16, GREATEST(p.quantity - p.used, 1)) - 1) g
) b
WHERE p.quantity >= 0
  AND NOT EXISTS (SELECT 1 FROM lottery_prize_stock s WHERE s.prize_id = p.id AND s.scope = 0)
ON CONFLICT DO NOTHING;

-- ── 风控聚集信号 ──
-- kind：1 设备号、2 IP、3 网段（IPv4 /24、IPv6 /64）。按天，保留 3 天。
CREATE TABLE IF NOT EXISTS lottery_risk_keys (
    activity_id BIGINT NOT NULL,
    day_start TIMESTAMPTZ NOT NULL,
    kind SMALLINT NOT NULL,
    key TEXT NOT NULL,
    user_id BIGINT NOT NULL,
    PRIMARY KEY (activity_id, day_start, kind, key, user_id)
);
CREATE INDEX IF NOT EXISTS idx_lottery_risk_keys_day ON lottery_risk_keys(day_start);

-- 一次性的数据补齐记一笔，补过的不再补（见 Repository.BackfillLotteryCounters）。
CREATE TABLE IF NOT EXISTS schema_backfills (
    name VARCHAR(64) PRIMARY KEY,
    done_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── lottery_draws 瘦身 ──
-- 抽奖不再按人、按奖品、按设备 / IP 聚合记录，这几个索引只剩写入成本（每抽一次都要更新）。
-- 留下的：主键、单号、幂等键、(activity_id, user_id, created_at)、(activity_id, created_at)、
-- (user_id, created_at)（删用户时级联删记录要用）、管理端检索用的那些。
DROP INDEX IF EXISTS idx_lottery_draws_user_prize;
DROP INDEX IF EXISTS idx_lottery_draws_activity_user;
DROP INDEX IF EXISTS idx_lottery_draws_ip;
DROP INDEX IF EXISTS idx_lottery_draws_device;

-- 出奖方案（trace）平均 1.6 KB，原样放在行内，表一半以上是它。改成 lz4 压缩、行超过 512 字节就移出行外：
-- 记录列表与统计扫的页数少一半以上。只影响新写入的行。
DO $$
BEGIN
    BEGIN
    IF (SELECT attcompression FROM pg_attribute
        WHERE attrelid = 'lottery_draws'::regclass AND attname = 'trace') IS DISTINCT FROM 'l' THEN
        ALTER TABLE lottery_draws ALTER COLUMN trace SET COMPRESSION lz4;
        ALTER TABLE lottery_draws ALTER COLUMN reward SET COMPRESSION lz4;
        ALTER TABLE lottery_draws ALTER COLUMN results SET COMPRESSION lz4;
    END IF;
    EXCEPTION WHEN feature_not_supported OR invalid_parameter_value THEN
        -- 没编进 lz4 的 Postgres：保持默认的 pglz
        NULL;
    END;
    IF NOT EXISTS (SELECT 1 FROM pg_class
                   WHERE oid = 'lottery_draws'::regclass AND 'toast_tuple_target=512' = ANY(reloptions)) THEN
        ALTER TABLE lottery_draws SET (toast_tuple_target = 512,
            autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02);
    END IF;
END $$;

-- 每抽一次都更新的行：页内留空位，更新走 HOT（不动索引、不产生索引膨胀）。只影响新页。
DO $$
DECLARE
    t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['lottery_user_states', 'lottery_draw_grants', 'lottery_draw_lots',
                             'lottery_activity_daily', 'lottery_prize_stock', 'lottery_prizes']
    LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE oid = t::regclass AND 'fillfactor=70' = ANY(reloptions)) THEN
            EXECUTE format('ALTER TABLE %I SET (fillfactor = 70, autovacuum_vacuum_scale_factor = 0.02)', t);
        END IF;
    END LOOP;
    IF NOT EXISTS (SELECT 1 FROM pg_class WHERE oid = 'users'::regclass AND 'fillfactor=85' = ANY(reloptions)) THEN
        ALTER TABLE users SET (fillfactor = 85);
    END IF;
END $$;

-- ── 风控留痕的去重 ──
-- 风控评估的留痕经 JetStream 流 AEGIS_RISK_TRAIL 异步写库（service/risk_trail.go）：消费者写完一批、提交之后才确认，
-- 进程崩溃时没确认的消息会重投。每条留痕带一个事件 ID，写库时先插进这张表，插得进去的才写记录、加计数，
-- 重投的那部分什么都不做。保留 8 天（比任何重投的间隔都长）。
CREATE TABLE IF NOT EXISTS risk_trail_events (
    event_id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_trail_events_created ON risk_trail_events(created_at);
