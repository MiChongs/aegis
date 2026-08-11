-- 扩展管理员审计日志，补齐请求追踪、地理、响应、分类、严重度等字段，使日志信息完整可读可分析
ALTER TABLE admin_audit_logs
    ADD COLUMN IF NOT EXISTS session_id       VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS request_id       VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS trace_id         VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS method           VARCHAR(12)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS path             VARCHAR(512) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS route            VARCHAR(512) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status_code      INTEGER      NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS latency_ms       INTEGER      NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS request_size     INTEGER      NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS response_size    INTEGER      NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS response_snippet TEXT         NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS country          VARCHAR(64)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS region           VARCHAR(64)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS city             VARCHAR(64)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS isp              VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS admin_role       VARCHAR(64)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS category         VARCHAR(32)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS severity         VARCHAR(16)  NOT NULL DEFAULT 'info',
    ADD COLUMN IF NOT EXISTS error_code       VARCHAR(64)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS error_message    TEXT         NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS summary          VARCHAR(1024) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_admin_audit_category    ON admin_audit_logs(category, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_admin_audit_severity    ON admin_audit_logs(severity, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_admin_audit_request     ON admin_audit_logs(request_id);
CREATE INDEX IF NOT EXISTS idx_admin_audit_session     ON admin_audit_logs(session_id);
CREATE INDEX IF NOT EXISTS idx_admin_audit_status_code ON admin_audit_logs(status_code, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_admin_audit_country     ON admin_audit_logs(country, created_at DESC);
