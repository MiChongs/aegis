-- 全站系统公告（仅管理员后台可见，与应用级通知完全独立）
CREATE TABLE IF NOT EXISTS system_announcements (
    id           BIGSERIAL    PRIMARY KEY,
    admin_id     BIGINT       NOT NULL REFERENCES admin_accounts(id),
    type         VARCHAR(32)  NOT NULL DEFAULT 'info',
    title        VARCHAR(256) NOT NULL,
    content      TEXT         NOT NULL DEFAULT '',
    level        VARCHAR(16)  NOT NULL DEFAULT 'normal',
    pinned       BOOLEAN      NOT NULL DEFAULT FALSE,
    status       VARCHAR(16)  NOT NULL DEFAULT 'draft',
    published_at TIMESTAMPTZ  NULL,
    expires_at   TIMESTAMPTZ  NULL,
    metadata     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT sys_ann_type_chk   CHECK (type   IN ('info','warning','maintenance','update','security')),
    CONSTRAINT sys_ann_level_chk  CHECK (level  IN ('normal','important','critical')),
    CONSTRAINT sys_ann_status_chk CHECK (status IN ('draft','published','archived'))
);

CREATE INDEX IF NOT EXISTS idx_sys_announcements_status_time
    ON system_announcements(status, pinned DESC, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_sys_announcements_active
    ON system_announcements(status, expires_at)
    WHERE status = 'published';
