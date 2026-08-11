-- +migrate Down

-- 先删内置订阅与渠道（订阅有 ON DELETE CASCADE，删渠道即可带走）
DELETE FROM notify_channels WHERE appid = 0 AND key IN ('inapp-default', 'realtime-default');

DROP TABLE IF EXISTS admin_notifications;
