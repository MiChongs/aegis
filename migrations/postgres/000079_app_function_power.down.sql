DROP INDEX IF EXISTS idx_app_function_kv_browse;
DROP INDEX IF EXISTS idx_app_function_invocations_status;

ALTER TABLE app_function_versions DROP COLUMN IF EXISTS notes;

ALTER TABLE app_functions DROP CONSTRAINT IF EXISTS app_functions_rate_limit_check;
ALTER TABLE app_functions DROP CONSTRAINT IF EXISTS app_functions_max_concurrency_check;
ALTER TABLE app_functions DROP COLUMN IF EXISTS rate_limit_per_min;
ALTER TABLE app_functions DROP COLUMN IF EXISTS max_concurrency;
ALTER TABLE app_functions DROP COLUMN IF EXISTS config;
