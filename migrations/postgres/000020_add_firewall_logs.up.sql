-- 防火墙安全日志表
CREATE TABLE IF NOT EXISTS firewall_logs (
    id              BIGSERIAL PRIMARY KEY,
    -- 请求信息
    request_id      TEXT NOT NULL DEFAULT '',
    ip              TEXT NOT NULL,
    method          TEXT NOT NULL DEFAULT '',
    path            TEXT NOT NULL DEFAULT '',
    query_string    TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    headers         JSONB,
    -- 拦截原因
    reason          TEXT NOT NULL,
    http_status     INT  NOT NULL DEFAULT 403,
    response_code   INT  NOT NULL DEFAULT 0,
    -- WAF 详情（仅 Coraza 拦截时填充）
    waf_rule_id     INT,
    waf_action      TEXT,
    waf_data        TEXT,
    -- GeoIP 信息（由 Worker 异步填充）
    country         TEXT NOT NULL DEFAULT '',
    country_code    TEXT NOT NULL DEFAULT '',
    region          TEXT NOT NULL DEFAULT '',
    city            TEXT NOT NULL DEFAULT '',
    isp             TEXT NOT NULL DEFAULT '',
    asn             TEXT NOT NULL DEFAULT '',
    timezone        TEXT NOT NULL DEFAULT '',
    latitude        DOUBLE PRECISION,
    longitude       DOUBLE PRECISION,
    -- 严重性
    severity        TEXT NOT NULL DEFAULT 'medium',
    -- 时间
    blocked_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 核心查询索引
CREATE INDEX IF NOT EXISTS idx_fw_logs_blocked_at        ON firewall_logs (blocked_at DESC);
CREATE INDEX IF NOT EXISTS idx_fw_logs_ip                ON firewall_logs (ip);
CREATE INDEX IF NOT EXISTS idx_fw_logs_reason            ON firewall_logs (reason);
CREATE INDEX IF NOT EXISTS idx_fw_logs_severity          ON firewall_logs (severity);
CREATE INDEX IF NOT EXISTS idx_fw_logs_country_code      ON firewall_logs (country_code);
CREATE INDEX IF NOT EXISTS idx_fw_logs_waf_rule_id       ON firewall_logs (waf_rule_id) WHERE waf_rule_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_fw_logs_blocked_at_reason ON firewall_logs (blocked_at DESC, reason);
