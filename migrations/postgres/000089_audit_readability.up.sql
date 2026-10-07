-- +migrate Up
-- 审计日志可读性：操作目录（internal/auditcatalog）接管模块与说法之后补的几列。
--
--   kind        read / write / export / auth —— 控制台默认只看「操作」，查看类请求另行切换
--   app_id/name 操作所在应用；写入时就记下名称，应用改名或删除后旧日志仍然看得懂
--   target_name 操作对象的名称（上传的文件名、handler 指定的对象名）
--   catalog_rev 写入或回填时的目录版本；目录改版后服务启动时按 (method, route) 回填模块与类型
--
-- 可重复执行。

ALTER TABLE admin_audit_logs
    ADD COLUMN IF NOT EXISTS kind        VARCHAR(16)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS app_id      BIGINT       NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS app_name    VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS target_name VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS catalog_rev INTEGER      NOT NULL DEFAULT 0;

-- 存量先按方法粗分，精确的模块与类型由服务启动时的目录回填覆盖
UPDATE admin_audit_logs SET kind = CASE WHEN method IN ('GET', 'HEAD') THEN 'read' ELSE 'write' END
 WHERE kind = '';

CREATE INDEX IF NOT EXISTS idx_admin_audit_kind_time ON admin_audit_logs(kind, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_admin_audit_app_time  ON admin_audit_logs(app_id, created_at DESC) WHERE app_id > 0;
CREATE INDEX IF NOT EXISTS idx_admin_audit_catalog   ON admin_audit_logs(catalog_rev) WHERE catalog_rev = 0;
