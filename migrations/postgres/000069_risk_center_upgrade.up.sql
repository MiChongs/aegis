-- 风控中心升级：可解释的评估记录 + 规则效果计数 + 实体画像
--
-- 三件事：
-- 1. 评估记录补齐**评估当时的上下文快照**（eval_context）。没有它，一条
--    「命中 3 条规则、判 block」的记录事后无法解释成因，复核人只能凭猜。
-- 2. 规则补齐命中计数，让「这条规则到底有没有在生效」可以被直接核对，
--    而不是翻日志或写 SQL 扫 JSONB。
-- 3. 补齐时间序列与反查所需的索引，大盘的趋势图与 IP/设备详情页依赖它们。

-- ── 规则：命中计数与修改人 ──
ALTER TABLE risk_rules ADD COLUMN IF NOT EXISTS hit_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE risk_rules ADD COLUMN IF NOT EXISTS last_hit_at TIMESTAMPTZ NULL;
ALTER TABLE risk_rules ADD COLUMN IF NOT EXISTS updated_by BIGINT NULL;

-- ── 评估记录：上下文与可检索维度 ──
ALTER TABLE risk_assessments ADD COLUMN IF NOT EXISTS account VARCHAR(190) NOT NULL DEFAULT '';
ALTER TABLE risk_assessments ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE risk_assessments ADD COLUMN IF NOT EXISTS country VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE risk_assessments ADD COLUMN IF NOT EXISTS eval_context JSONB NOT NULL DEFAULT '{}';
ALTER TABLE risk_assessments ADD COLUMN IF NOT EXISTS latency_ms INT NOT NULL DEFAULT 0;

-- 时间序列聚合（大盘趋势图）
CREATE INDEX IF NOT EXISTS idx_risk_assessments_created ON risk_assessments(created_at DESC);
-- 实体反查（IP / 设备 / 账号 详情页）
CREATE INDEX IF NOT EXISTS idx_risk_assessments_ip ON risk_assessments(ip, created_at DESC) WHERE ip <> '';
CREATE INDEX IF NOT EXISTS idx_risk_assessments_device ON risk_assessments(device_id, created_at DESC) WHERE device_id <> '';
CREATE INDEX IF NOT EXISTS idx_risk_assessments_account ON risk_assessments(account, created_at DESC) WHERE account <> '';
-- 等级 / 动作筛选
CREATE INDEX IF NOT EXISTS idx_risk_assessments_level ON risk_assessments(risk_level, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_risk_assessments_action ON risk_assessments(action, created_at DESC);
-- 「某条规则最近命中了哪些请求」：matched_rules 是 JSONB 数组，GIN 才能走索引
CREATE INDEX IF NOT EXISTS idx_risk_assessments_matched ON risk_assessments USING GIN (matched_rules jsonb_path_ops);

-- ── 设备指纹：最近一次的网络与客户端事实 ──
ALTER TABLE device_fingerprints ADD COLUMN IF NOT EXISTS last_ip VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE device_fingerprints ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE device_fingerprints ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_device_fingerprints_last_seen ON device_fingerprints(last_seen_at DESC);

-- ── IP 风险库：情报来源与人工备注 ──
-- source 区分「外部情报源写的」与「人工标注的」：没有这一列，
-- 一次情报刷新就会把管理员的人工结论悄悄覆盖掉。
ALTER TABLE ip_risk_records ADD COLUMN IF NOT EXISTS asn VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE ip_risk_records ADD COLUMN IF NOT EXISTS source VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE ip_risk_records ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_ip_risk_last_seen ON ip_risk_records(last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_ip_risk_score ON ip_risk_records(risk_score DESC);

-- ── 处置策略：区间重叠是配置事故的主要来源，加一条约束挡住 min > max ──
ALTER TABLE risk_actions DROP CONSTRAINT IF EXISTS chk_risk_actions_score_range;
ALTER TABLE risk_actions ADD CONSTRAINT chk_risk_actions_score_range
    CHECK (max_score IS NULL OR max_score >= min_score);
