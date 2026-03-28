CREATE TABLE IF NOT EXISTS admin_audit_logs (
    id          BIGSERIAL    PRIMARY KEY,
    admin_id    BIGINT       NOT NULL,
    admin_name  VARCHAR(128) NOT NULL DEFAULT '',
    action      VARCHAR(64)  NOT NULL,
    resource    VARCHAR(64)  NOT NULL DEFAULT '',
    resource_id VARCHAR(128) NOT NULL DEFAULT '',
    detail      TEXT         NOT NULL DEFAULT '',
    changes     JSONB        NULL,
    ip          VARCHAR(45)  NOT NULL DEFAULT '',
    user_agent  TEXT         NOT NULL DEFAULT '',
    status      VARCHAR(16)  NOT NULL DEFAULT 'success',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_admin_audit_admin ON admin_audit_logs(admin_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_admin_audit_action ON admin_audit_logs(action, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_admin_audit_resource ON admin_audit_logs(resource, resource_id);
CREATE INDEX IF NOT EXISTS idx_admin_audit_time ON admin_audit_logs(created_at DESC);
