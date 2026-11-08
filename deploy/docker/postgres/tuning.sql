-- Aegis 生产库的服务端参数（docs/database.md「库侧参数」）。
--
-- 按 8 核 / 32 GB、库与 API 及其它服务同机、云盘（SSD）估算。ALTER SYSTEM 写进数据目录里的
-- postgresql.auto.conf，容器重建后仍然生效（前提是容器启动命令里没有用 -c 覆盖同名参数）。
--
-- 用法：
--   docker exec -i <postgres 容器> psql -U <用户> -d postgres < tuning.sql
-- 之后：
--   标「重启」的参数要重启 Postgres 才生效（约几秒中断，API 会自动重连）；
--   其余参数 SELECT pg_reload_conf() 立即生效（脚本末尾已经调用）。
-- 容器的 /dev/shm 默认只有 64 MB，并行查询的动态共享内存默认在那里：下面的 min_dynamic_shared_memory 在启动时从主共享内存里
-- 先留出一块给它（不走 /dev/shm），改不了 compose 的 shm_size 时这样就够；能改的话 compose 里再给 postgres 加 shm_size: 1g。

-- ── 内存 ──
ALTER SYSTEM SET shared_buffers = '4GB';              -- 重启。默认 128 MB：600 MB 的抽奖表都放不进缓存
ALTER SYSTEM SET effective_cache_size = '16GB';       -- 规划器估算可用的页缓存
ALTER SYSTEM SET work_mem = '16MB';                   -- 每个排序 / 哈希节点；连接数 × 节点数要留余量
ALTER SYSTEM SET maintenance_work_mem = '512MB';      -- 建索引、VACUUM
ALTER SYSTEM SET huge_pages = 'try';                  -- 重启
ALTER SYSTEM SET min_dynamic_shared_memory = '256MB'; -- 重启。并行查询先用这块，不受容器 /dev/shm 64 MB 的限制

-- ── 连接 ──
ALTER SYSTEM SET max_connections = 200;               -- 重启。API 每实例最多 40 条，滚动发布时两个实例同时在线
ALTER SYSTEM SET idle_in_transaction_session_timeout = '60s';  -- 兜底：任何客户端都不能开着事务发呆
ALTER SYSTEM SET deadlock_timeout = '1s';

-- ── 写入与检查点 ──
ALTER SYSTEM SET wal_buffers = '64MB';                -- 重启
ALTER SYSTEM SET max_wal_size = '4GB';
ALTER SYSTEM SET min_wal_size = '1GB';
ALTER SYSTEM SET checkpoint_timeout = '15min';
ALTER SYSTEM SET checkpoint_completion_target = 0.9;
ALTER SYSTEM SET wal_compression = 'lz4';

-- ── 规划器（云盘随机读不比顺序读慢多少）──
ALTER SYSTEM SET random_page_cost = 1.1;
ALTER SYSTEM SET effective_io_concurrency = 200;

-- ── 自动清理（抽奖、积分这类每秒写入的表）──
ALTER SYSTEM SET autovacuum_max_workers = 4;          -- 重启
ALTER SYSTEM SET autovacuum_naptime = '15s';
ALTER SYSTEM SET autovacuum_vacuum_cost_limit = 2000;
ALTER SYSTEM SET autovacuum_vacuum_scale_factor = 0.05;
ALTER SYSTEM SET autovacuum_analyze_scale_factor = 0.02;

-- ── 排障 ──
ALTER SYSTEM SET shared_preload_libraries = 'pg_stat_statements';  -- 重启；之后 CREATE EXTENSION pg_stat_statements
ALTER SYSTEM SET pg_stat_statements.max = 5000;
ALTER SYSTEM SET track_io_timing = on;
ALTER SYSTEM SET log_lock_waits = on;                 -- 等锁超过 deadlock_timeout 记一条：下次锁冲突能看到是谁占着
ALTER SYSTEM SET log_min_duration_statement = '1s';   -- 慢于 1 秒的语句记日志
ALTER SYSTEM SET log_autovacuum_min_duration = '10s';

SELECT pg_reload_conf();
SELECT name, setting, unit, pending_restart FROM pg_settings WHERE pending_restart ORDER BY name;
