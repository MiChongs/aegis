-- 为 ip_bans 表添加 mode 列，支持多种封禁响应模式
--   forbidden        : 返回 HTTP 403 + JSON 错误信封（默认）
--   silent_drop      : 劫持 TCP 连接直接关闭（完全不响应）
--   connection_reset : 同 silent_drop
--   tarpit           : 延迟 N 秒再返回 403
--   stealth_404      : 伪装 404
--   teapot           : 返回 HTTP 418
--   rate_choke       : 极严格限速

ALTER TABLE ip_bans
    ADD COLUMN IF NOT EXISTS mode VARCHAR(32) NOT NULL DEFAULT '';

COMMENT ON COLUMN ip_bans.mode IS '封禁响应模式；空字符串=使用全局默认模式';
