-- apps.id 改为自增序列（保留已有数据），app_key 改为 NOT NULL UNIQUE 自动生成

-- 1) 创建序列，从当前最大 id + 1 开始
CREATE SEQUENCE IF NOT EXISTS apps_id_seq AS BIGINT;
SELECT setval('apps_id_seq', COALESCE((SELECT MAX(id) FROM apps), 10000));
ALTER TABLE apps ALTER COLUMN id SET DEFAULT nextval('apps_id_seq');

-- 2) 为已有行填充 app_key（如果为空）
UPDATE apps SET app_key = gen_random_uuid()::text WHERE app_key IS NULL OR app_key = '';

-- 3) app_key 设为 NOT NULL + 默认值 + 唯一索引
ALTER TABLE apps ALTER COLUMN app_key SET NOT NULL;
ALTER TABLE apps ALTER COLUMN app_key SET DEFAULT gen_random_uuid()::text;
CREATE UNIQUE INDEX IF NOT EXISTS uk_apps_app_key ON apps (app_key);
