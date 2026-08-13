-- 回滚平台级邮件通道。
--
-- 平台级配置（appid IS NULL）必须先删掉：NOT NULL 加不回去，
-- 而留着它们又会让旧代码把一条不属于任何应用的配置当成应用级去读。
DELETE FROM email_deliveries WHERE appid IS NULL;
DELETE FROM app_email_configs WHERE appid IS NULL;

DROP INDEX IF EXISTS idx_email_deliveries_purpose;
DROP INDEX IF EXISTS idx_email_deliveries_platform_created;
ALTER TABLE email_deliveries ALTER COLUMN appid SET NOT NULL;

DROP INDEX IF EXISTS idx_app_email_configs_shared;
DROP INDEX IF EXISTS idx_app_email_configs_platform;
DROP INDEX IF EXISTS uq_app_email_configs_scope_default;
DROP INDEX IF EXISTS uq_app_email_configs_scope_name;

ALTER TABLE app_email_configs DROP CONSTRAINT IF EXISTS ck_app_email_configs_shared;
ALTER TABLE app_email_configs DROP COLUMN IF EXISTS shared;
ALTER TABLE app_email_configs ALTER COLUMN appid SET NOT NULL;
ALTER TABLE app_email_configs ADD CONSTRAINT app_email_configs_appid_name_key UNIQUE (appid, name);
