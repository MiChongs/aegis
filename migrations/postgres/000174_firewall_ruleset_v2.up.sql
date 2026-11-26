-- +migrate Up
-- 防火墙规则集 v2（docs/firewall.md）：事件表补动作 / 阶段 / 载荷来源 / 命中明细，
-- 新增自定义规则与 WAF 例外两张表。

-- ① firewall_logs：观察模式「本该拦截」、自定义规则的记录动作、出站检测也落这张表，
--    统计拦截数的查询一律带 action = 'block'。旧数据全是拦截，默认值即 block。
ALTER TABLE firewall_logs
    ADD COLUMN IF NOT EXISTS action        TEXT    NOT NULL DEFAULT 'block',
    ADD COLUMN IF NOT EXISTS stage         TEXT    NOT NULL DEFAULT 'edge',
    ADD COLUMN IF NOT EXISTS transport     TEXT    NOT NULL DEFAULT 'plain',
    ADD COLUMN IF NOT EXISTS app_key       TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS anomaly_score INT     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS matches       JSONB,
    ADD COLUMN IF NOT EXISTS rule_ref      BIGINT,
    ADD COLUMN IF NOT EXISTS rule_name     TEXT    NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_fw_logs_action_blocked_at ON firewall_logs (action, blocked_at DESC);
CREATE INDEX IF NOT EXISTS idx_fw_logs_ip_action_blocked ON firewall_logs (ip, action, blocked_at DESC);
CREATE INDEX IF NOT EXISTS idx_fw_logs_rule_ref ON firewall_logs (rule_ref) WHERE rule_ref IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_fw_logs_app_key ON firewall_logs (app_key, blocked_at DESC) WHERE app_key <> '';
CREATE INDEX IF NOT EXISTS idx_fw_logs_matches ON firewall_logs USING GIN (matches jsonb_path_ops);

-- ② 自定义规则：expr 表达式 + 动作，按 priority 升序求值。
CREATE TABLE IF NOT EXISTS firewall_rules (
    id           BIGSERIAL PRIMARY KEY,
    name         TEXT        NOT NULL,
    description  TEXT        NOT NULL DEFAULT '',
    enabled      BOOLEAN     NOT NULL DEFAULT TRUE,
    priority     INT         NOT NULL DEFAULT 100,
    expression   TEXT        NOT NULL,
    action       TEXT        NOT NULL CHECK (action IN ('allow', 'block', 'log', 'skip_waf')),
    block_mode   TEXT        NOT NULL DEFAULT '',
    ban_seconds  BIGINT      NOT NULL DEFAULT 0,
    hits         BIGINT      NOT NULL DEFAULT 0,
    last_hit_at  TIMESTAMPTZ,
    created_by   BIGINT,
    updated_by   BIGINT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_firewall_rules_priority ON firewall_rules (priority, id);

-- ③ WAF 例外：按路径（前缀 / 精确 / 正则）+ 方法，移除规则或只移除某些检查目标。
CREATE TABLE IF NOT EXISTS firewall_waf_exclusions (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL,
    note        TEXT        NOT NULL DEFAULT '',
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    position    INT         NOT NULL DEFAULT 0,
    path_match  TEXT        NOT NULL CHECK (path_match IN ('prefix', 'exact', 'regex')),
    path        TEXT        NOT NULL,
    methods     TEXT[]      NOT NULL DEFAULT '{}',
    rule_ids    INT[]       NOT NULL DEFAULT '{}',
    tags        TEXT[]      NOT NULL DEFAULT '{}',
    targets     TEXT[]      NOT NULL DEFAULT '{}',
    created_by  BIGINT,
    updated_by  BIGINT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_firewall_waf_exclusions_position ON firewall_waf_exclusions (position, id);
