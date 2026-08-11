DROP TABLE IF EXISTS app_function_kv;

-- 回滚前必须先清理 script 版本与函数，否则重建旧约束会失败。
DELETE FROM app_function_versions WHERE source <> '';
DELETE FROM app_functions WHERE runtime = 'script';

ALTER TABLE app_function_versions DROP CONSTRAINT IF EXISTS app_function_versions_artifact_check;
ALTER TABLE app_function_versions DROP COLUMN IF EXISTS source;
ALTER TABLE app_function_versions
    ADD CONSTRAINT app_function_versions_check
    CHECK (
        (endpoint_url <> '' AND wasm_module IS NULL)
        OR (endpoint_url = '' AND wasm_module IS NOT NULL)
    );

ALTER TABLE app_functions DROP CONSTRAINT IF EXISTS app_functions_runtime_check;
ALTER TABLE app_functions
    ADD CONSTRAINT app_functions_runtime_check
    CHECK (runtime IN ('wasm', 'http'));
