-- ── 邮件投递留痕 ──
-- 每封外发邮件一行。SMTP 通道交出信件即停在 sent（协议没有回执），
-- 只有 Zeabur Email 的 webhook 能把状态继续推进到 delivered / bounced / complained。
CREATE TABLE IF NOT EXISTS email_deliveries (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    config_id BIGINT REFERENCES app_email_configs(id) ON DELETE SET NULL,
    config_name VARCHAR(100),
    provider VARCHAR(32) NOT NULL DEFAULT 'smtp',
    -- 服务商侧的邮件 ID（Zeabur 的 email id），是 webhook 回填时的唯一关联键。
    -- 与下面的 message_id（RFC 5322 Message-ID）不同源，不可混用。
    provider_message_id VARCHAR(128),
    message_id VARCHAR(255),
    to_address VARCHAR(320) NOT NULL,
    from_address VARCHAR(320),
    subject VARCHAR(998),
    purpose VARCHAR(64),
    -- pending / sent / delivered / bounced / complained / rejected / failed
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    error_message TEXT,
    open_count INTEGER NOT NULL DEFAULT 0,
    click_count INTEGER NOT NULL DEFAULT 0,
    last_event VARCHAR(32),
    last_event_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    sent_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- webhook 按 provider_message_id 定位记录，必须唯一；
-- SMTP 通道该列为 NULL，故用部分索引避免 NULL 冲突。
CREATE UNIQUE INDEX IF NOT EXISTS uq_email_deliveries_provider_message
    ON email_deliveries(provider_message_id)
    WHERE provider_message_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_email_deliveries_appid_created
    ON email_deliveries(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_email_deliveries_status
    ON email_deliveries(appid, status);
CREATE INDEX IF NOT EXISTS idx_email_deliveries_config
    ON email_deliveries(config_id);
CREATE INDEX IF NOT EXISTS idx_email_deliveries_to
    ON email_deliveries(appid, to_address);
