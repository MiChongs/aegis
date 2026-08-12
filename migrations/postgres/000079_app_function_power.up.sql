-- 远程函数补齐：函数级配置、并发与频次闸门、版本发版说明。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行
-- （与 000059 同一约束：约束先 DROP IF EXISTS 再 ADD，否则第二次执行报 42710）。

-- 函数级参数。脚本里读作 `aegis.config`，控制台上可随时改。
--
-- 它存在的理由是「改一个阈值不该需要发一个新版本」：版本记录不可变是对的，
-- 但把「每日额度 100」这种数字也钉进不可变产物里，等于每次调参都要走一遍
-- 发版 + 激活，而回滚时还会把无关的逻辑一起滚回去。
-- 永不下发给接入方，与脚本正文同级。
ALTER TABLE app_functions ADD COLUMN IF NOT EXISTS config JSONB NOT NULL DEFAULT '{}'::jsonb;

-- 单实例并发上限。原先硬编码为 8，且没有任何地方说得出这个数 ——
-- 一个 20ms 的脚本和一个 3s 的 HTTP 转发不该共用同一个闸门。
ALTER TABLE app_functions ADD COLUMN IF NOT EXISTS max_concurrency INTEGER NOT NULL DEFAULT 8;
ALTER TABLE app_functions DROP CONSTRAINT IF EXISTS app_functions_max_concurrency_check;
ALTER TABLE app_functions
    ADD CONSTRAINT app_functions_max_concurrency_check
    CHECK (max_concurrency BETWEEN 1 AND 64);

-- 每分钟调用上限，0 表示不限。
--
-- 与 max_concurrency 是两件事：并发是「同时能跑几个」（保护本进程，天然按实例算），
-- 频次是「一分钟能跑几次」（业务配额，必须跨实例准确）。因此计数落在
-- app_function_kv 上走数据库原子自增，而不是各实例内存里各记一份 ——
-- 后者在多实例部署下的表现是「配了 60/分钟，实际放行 60×实例数」，
-- 且控制台上完全看不出来。
ALTER TABLE app_functions ADD COLUMN IF NOT EXISTS rate_limit_per_min INTEGER NOT NULL DEFAULT 0;
ALTER TABLE app_functions DROP CONSTRAINT IF EXISTS app_functions_rate_limit_check;
ALTER TABLE app_functions
    ADD CONSTRAINT app_functions_rate_limit_check
    CHECK (rate_limit_per_min BETWEEN 0 AND 600000);

-- 发版说明。版本列表原先只有版本号与 SHA-256，回答不了「这一版改了什么」，
-- 而回滚时恰恰要靠它决定滚到哪一版。
ALTER TABLE app_function_versions ADD COLUMN IF NOT EXISTS notes TEXT NOT NULL DEFAULT '';

-- 调用审计按状态筛选（排障看的是失败的那几条，不是最近 50 条）。
CREATE INDEX IF NOT EXISTS idx_app_function_invocations_status
    ON app_function_invocations(appid, function_id, status, created_at DESC);

-- KV 浏览器按前缀检索。
CREATE INDEX IF NOT EXISTS idx_app_function_kv_browse
    ON app_function_kv(appid, scope, key);
