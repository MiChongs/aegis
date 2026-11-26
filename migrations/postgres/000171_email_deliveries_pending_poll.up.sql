-- +migrate Up
-- 不推送回执的邮件服务商（Cloud Mail）靠定时对账回填投递状态。
-- 对账只看「还没终态」的信：部分索引只收 pending / sent，表再大也只扫一小段。
CREATE INDEX IF NOT EXISTS idx_email_deliveries_pending_poll
    ON email_deliveries(provider, created_at DESC)
    WHERE status IN ('pending', 'sent') AND provider_message_id IS NOT NULL;
