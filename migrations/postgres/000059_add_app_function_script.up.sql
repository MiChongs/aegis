-- 应用函数增加 script 运行时：脚本正文存在服务端，由 Aegis 进程内的 JS 沙箱执行。
--
-- 与既有的 wasm / http 运行时并存：
--   wasm   —— 纯计算，无宿主能力（保留，适合确定性算法）
--   http   —— 转发到接入方自建 HTTPS 端点（保留，适合已有微服务）
--   script —— 服务端脚本，可通过受控 SDK 读写平台数据（新增，自定义 API 的主路径）

ALTER TABLE app_functions DROP CONSTRAINT IF EXISTS app_functions_runtime_check;
ALTER TABLE app_functions
    ADD CONSTRAINT app_functions_runtime_check
    CHECK (runtime IN ('wasm', 'http', 'script'));

-- 脚本正文。与 wasm_module / endpoint_url 一样属于版本的不可变产物。
ALTER TABLE app_function_versions ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '';

-- 原约束是「endpoint_url 与 wasm_module 二选一」，现放宽为三选一。
-- 迁移器每次启动都会重跑全部 *.up.sql，因此新名字也要先 DROP IF EXISTS，
-- 否则第二次执行会因约束已存在而报 42710（与上面 runtime_check 同一写法）。
ALTER TABLE app_function_versions DROP CONSTRAINT IF EXISTS app_function_versions_check;
ALTER TABLE app_function_versions DROP CONSTRAINT IF EXISTS app_function_versions_artifact_check;
ALTER TABLE app_function_versions
    ADD CONSTRAINT app_function_versions_artifact_check
    CHECK (
        (endpoint_url <> '' AND wasm_module IS NULL  AND source =  '')
     OR (endpoint_url =  '' AND wasm_module IS NOT NULL AND source =  '')
     OR (endpoint_url =  '' AND wasm_module IS NULL  AND source <> '')
    );

-- 脚本可用的键值存储。
--
-- 这是「服务端独占状态」的载体：计数器、频次限制、服务端下发的配置与密钥。
-- 客户端无法读取也无法伪造，脚本逻辑一旦依赖它，就无法在本地被复现。
CREATE TABLE IF NOT EXISTS app_function_kv (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- app  = 应用级共享；user = 按调用者隔离，脚本无法跨用户读写
    scope VARCHAR(8) NOT NULL CHECK (scope IN ('app', 'user')),
    -- scope='user' 时为 users.id；scope='app' 时固定为 0
    scope_id BIGINT NOT NULL DEFAULT 0,
    key VARCHAR(128) NOT NULL,
    value JSONB NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (appid, scope, scope_id, key)
);

CREATE INDEX IF NOT EXISTS idx_app_function_kv_expiry
    ON app_function_kv(appid, expires_at)
    WHERE expires_at IS NOT NULL;
