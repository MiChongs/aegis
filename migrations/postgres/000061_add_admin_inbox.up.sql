-- +migrate Up
-- 管理员收件箱 + 站内信/实时推送的开箱即用配置
--
-- 为什么要单独一张表：
--   notifications.user_id 外键指向 users(id)，那是「应用用户」的收件箱。
--   管理员存在 admin_accounts，与 users 是两套主键空间 —— 把 admin_id 写进 notifications
--   要么违反外键，要么静默命中一个同号的应用用户（跨租户串消息）。
--   因此管理员侧必须有自己的收件箱表。

CREATE TABLE IF NOT EXISTS admin_notifications (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    -- 业务域：ticket / system / security / org ...
    type VARCHAR(64) NOT NULL DEFAULT 'system',
    title VARCHAR(256) NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    level VARCHAR(16) NOT NULL DEFAULT 'info' CHECK (level IN ('info','success','warning','critical')),
    status VARCHAR(16) NOT NULL DEFAULT 'unread' CHECK (status IN ('unread','read')),

    -- 关联业务对象，点击通知直达详情
    resource VARCHAR(32) NOT NULL DEFAULT '',
    resource_id VARCHAR(64) NOT NULL DEFAULT '',
    -- 控制台内部路径（如 /tickets?id=42）；只存相对路径，避免域名变更后失效
    link TEXT NOT NULL DEFAULT '',

    -- 幂等键：同一事件对同一管理员只投一次
    dedupe_key VARCHAR(160) NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at TIMESTAMPTZ NULL
);

-- 收件箱主查询：某管理员的时间线倒序
CREATE INDEX IF NOT EXISTS idx_admin_notifications_inbox
    ON admin_notifications(admin_id, created_at DESC);
-- 未读角标：只扫未读行
CREATE INDEX IF NOT EXISTS idx_admin_notifications_unread
    ON admin_notifications(admin_id) WHERE status = 'unread';
-- 按业务对象回查（工单详情页展示"谁被通知过"）
CREATE INDEX IF NOT EXISTS idx_admin_notifications_resource
    ON admin_notifications(resource, resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_admin_notifications_dedupe
    ON admin_notifications(dedupe_key) WHERE dedupe_key IS NOT NULL;

-- ─────────────────────────── 内置渠道与订阅 ───────────────────────────
-- 站内信与实时推送是产品内建行为，不该要求管理员先手工建渠道才生效。
-- 这里预置平台级渠道 + ticket.* 订阅；不需要的话在控制台停用即可。

INSERT INTO notify_channels (appid, key, name, kind, config, enabled)
SELECT 0, 'inapp-default', '站内信（内置）', 'inapp', '{"type":"ticket"}'::jsonb, TRUE
WHERE NOT EXISTS (SELECT 1 FROM notify_channels WHERE appid = 0 AND key = 'inapp-default');

INSERT INTO notify_channels (appid, key, name, kind, config, enabled)
SELECT 0, 'realtime-default', '实时推送（内置）', 'realtime', '{}'::jsonb, TRUE
WHERE NOT EXISTS (SELECT 1 FROM notify_channels WHERE appid = 0 AND key = 'realtime-default');

INSERT INTO notify_subscriptions (channel_id, event_key, enabled)
SELECT c.id, 'ticket.*', TRUE
FROM notify_channels c
WHERE c.appid = 0
  AND c.key IN ('inapp-default', 'realtime-default')
  AND NOT EXISTS (
      SELECT 1 FROM notify_subscriptions s
      WHERE s.channel_id = c.id AND s.event_key = 'ticket.*'
  );
