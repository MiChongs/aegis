-- +migrate Up
-- 工单系统 + 统一通知出口（飞书 / 钉钉 / 企微 / Slack / Webhook / 邮件 / 站内信 / 实时推送）
--
-- 设计要点：
--   1. 工单同时服务两类提单人：应用用户（requester_type='user'）与管理员（'admin'）
--   2. 处理权限分三层：全局权限点(ticket:*) → 应用范围(assignments.appid) → 人员归属(受理人/处理组/关注人)
--      "特定人员"即 ticket_groups 成员或被指派人，无需全局权限也能处理自己名下的工单
--   3. 通知走统一出口：notify_channels(渠道实例) + notify_subscriptions(事件路由) + notify_deliveries(投递留痕)
--      业务侧只发事件 key，不关心渠道差异

-- ─────────────────────────── 工单基础配置 ───────────────────────────

-- SLA 策略：按优先级配置首次响应 / 解决时限（分钟），JSONB 形如 {"urgent":30,"high":120,...}
CREATE TABLE IF NOT EXISTS ticket_sla_policies (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL DEFAULT 0,               -- 0 = 平台级策略，可被所有应用复用
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    first_response_minutes JSONB NOT NULL DEFAULT '{"urgent":30,"high":120,"normal":480,"low":1440}'::jsonb,
    resolve_minutes JSONB NOT NULL DEFAULT '{"urgent":240,"high":720,"normal":2880,"low":7200}'::jsonb,
    -- 仅在工作时间内计时；business_hours 形如 {"timezone":"Asia/Shanghai","days":[1,2,3,4,5],"start":"09:00","end":"18:00"}
    business_hours JSONB NULL,
    warn_ratio NUMERIC(4,2) NOT NULL DEFAULT 0.80 CHECK (warn_ratio > 0 AND warn_ratio < 1),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ticket_sla_app ON ticket_sla_policies(appid);

-- 处理组：把"特定人员"编成组，工单可以直接指派到组
CREATE TABLE IF NOT EXISTS ticket_groups (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL DEFAULT 0,               -- 0 = 平台级处理组，可承接任意应用工单
    key VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- 轮询指派游标：round_robin 时记录上次分配到的成员序号
    assign_strategy VARCHAR(16) NOT NULL DEFAULT 'manual',  -- manual / round_robin / least_open
    round_robin_cursor INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_ticket_groups_key UNIQUE (appid, key)
);

CREATE TABLE IF NOT EXISTS ticket_group_members (
    group_id BIGINT NOT NULL REFERENCES ticket_groups(id) ON DELETE CASCADE,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    role VARCHAR(16) NOT NULL DEFAULT 'agent',     -- agent / leader（leader 可越权处理组内全部工单）
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, admin_id)
);

CREATE INDEX IF NOT EXISTS idx_ticket_group_members_admin ON ticket_group_members(admin_id);

-- 工单分类：决定默认优先级 / 默认处理组 / SLA
CREATE TABLE IF NOT EXISTS ticket_categories (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL DEFAULT 0,               -- 0 = 平台级分类（如"账号申诉""平台故障"）
    parent_id BIGINT NULL REFERENCES ticket_categories(id) ON DELETE SET NULL,
    key VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    default_priority VARCHAR(16) NOT NULL DEFAULT 'normal',
    default_group_id BIGINT NULL REFERENCES ticket_groups(id) ON DELETE SET NULL,
    sla_policy_id BIGINT NULL REFERENCES ticket_sla_policies(id) ON DELETE SET NULL,
    -- 提单表单的自定义字段定义（JSON Schema 简化版），前端据此动态渲染
    form_schema JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- 是否允许应用用户自助提单（false = 仅管理员可代提）
    user_submittable BOOLEAN NOT NULL DEFAULT TRUE,
    sort INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_ticket_categories_key UNIQUE (appid, key)
);

CREATE INDEX IF NOT EXISTS idx_ticket_categories_app ON ticket_categories(appid, sort ASC);

-- ─────────────────────────── 工单主体 ───────────────────────────

-- 工单号流水：单调自增，避免用 COUNT(*) 生成导致并发撞号 / 删除后重号
CREATE SEQUENCE IF NOT EXISTS ticket_no_seq;

CREATE TABLE IF NOT EXISTS tickets (
    id BIGSERIAL PRIMARY KEY,
    ticket_no VARCHAR(32) NOT NULL,
    appid BIGINT NOT NULL DEFAULT 0,               -- 0 = 平台内部工单（管理员之间）

    -- 提单人：user（应用用户）/ admin（管理员）二选一
    requester_type VARCHAR(8) NOT NULL DEFAULT 'user' CHECK (requester_type IN ('user','admin')),
    requester_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    requester_admin_id BIGINT NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    requester_name VARCHAR(128) NOT NULL DEFAULT '',
    requester_contact VARCHAR(256) NOT NULL DEFAULT '',   -- 邮箱 / 手机 / 其它回访方式

    category_id BIGINT NULL REFERENCES ticket_categories(id) ON DELETE SET NULL,
    title VARCHAR(256) NOT NULL,
    -- 状态机：open → processing → (pending_user) → resolved → closed，closed/resolved 可 reopened
    status VARCHAR(24) NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','processing','pending_user','pending_third_party','resolved','closed','cancelled')),
    priority VARCHAR(16) NOT NULL DEFAULT 'normal'
        CHECK (priority IN ('low','normal','high','urgent')),
    source VARCHAR(24) NOT NULL DEFAULT 'console'  -- console / app / api / email / bot / import
        CHECK (source IN ('console','app','api','email','bot','import')),

    -- 归属
    assignee_admin_id BIGINT NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    group_id BIGINT NULL REFERENCES ticket_groups(id) ON DELETE SET NULL,

    -- SLA
    sla_policy_id BIGINT NULL REFERENCES ticket_sla_policies(id) ON DELETE SET NULL,
    first_response_due_at TIMESTAMPTZ NULL,
    resolve_due_at TIMESTAMPTZ NULL,
    first_responded_at TIMESTAMPTZ NULL,
    resolved_at TIMESTAMPTZ NULL,
    closed_at TIMESTAMPTZ NULL,
    sla_state VARCHAR(16) NOT NULL DEFAULT 'ontime'  -- ontime / warning / breached / paused / met
        CHECK (sla_state IN ('ontime','warning','breached','paused','met')),

    -- 会话冗余（列表页避免 JOIN）
    message_count INTEGER NOT NULL DEFAULT 0,
    last_message_at TIMESTAMPTZ NULL,
    last_message_role VARCHAR(16) NOT NULL DEFAULT '',  -- requester / agent / system
    reopened_count INTEGER NOT NULL DEFAULT 0,

    -- 满意度
    rating SMALLINT NULL CHECK (rating BETWEEN 1 AND 5),
    rating_comment TEXT NOT NULL DEFAULT '',
    rated_at TIMESTAMPTZ NULL,

    tags TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    -- 自定义表单填写值 + 来源上下文（IP/UA/设备/版本）
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- 关闭后是否禁止继续回复
    locked BOOLEAN NOT NULL DEFAULT FALSE,

    created_by_admin_id BIGINT NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_tickets_no UNIQUE (ticket_no),
    -- 提单人必须能被追溯：user 型必须有 user_id，admin 型必须有 admin_id
    CONSTRAINT ck_tickets_requester CHECK (
        (requester_type = 'user'  AND requester_user_id IS NOT NULL) OR
        (requester_type = 'admin' AND requester_admin_id IS NOT NULL)
    )
);

-- 列表主索引：按应用 + 状态 + 更新时间倒序
CREATE INDEX IF NOT EXISTS idx_tickets_app_status ON tickets(appid, status, updated_at DESC);
-- "我的待办"：受理人 + 未终结状态
CREATE INDEX IF NOT EXISTS idx_tickets_assignee_open ON tickets(assignee_admin_id, updated_at DESC)
    WHERE status NOT IN ('closed','cancelled');
CREATE INDEX IF NOT EXISTS idx_tickets_group_open ON tickets(group_id, updated_at DESC)
    WHERE status NOT IN ('closed','cancelled');
CREATE INDEX IF NOT EXISTS idx_tickets_requester_user ON tickets(requester_user_id, created_at DESC)
    WHERE requester_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tickets_requester_admin ON tickets(requester_admin_id, created_at DESC)
    WHERE requester_admin_id IS NOT NULL;
-- SLA 扫描：只关心尚未终结且有时限的工单
CREATE INDEX IF NOT EXISTS idx_tickets_sla_scan ON tickets(first_response_due_at, resolve_due_at)
    WHERE status NOT IN ('closed','cancelled','resolved');
CREATE INDEX IF NOT EXISTS idx_tickets_category ON tickets(category_id);
CREATE INDEX IF NOT EXISTS idx_tickets_tags ON tickets USING GIN(tags);
CREATE INDEX IF NOT EXISTS idx_tickets_created ON tickets(created_at DESC);

-- ─────────────────────────── 工单会话 ───────────────────────────

CREATE TABLE IF NOT EXISTS ticket_messages (
    id BIGSERIAL PRIMARY KEY,
    ticket_id BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    -- requester：提单人可见的对外发言；agent：处理人对外回复；system：状态变更等系统消息
    author_type VARCHAR(16) NOT NULL CHECK (author_type IN ('requester','agent','system')),
    author_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    author_admin_id BIGINT NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    author_name VARCHAR(128) NOT NULL DEFAULT '',
    -- internal=TRUE 为内部备注：仅拥有 ticket:internal 的管理员可见，永不下发给提单人
    internal BOOLEAN NOT NULL DEFAULT FALSE,
    content TEXT NOT NULL,
    content_type VARCHAR(16) NOT NULL DEFAULT 'text' CHECK (content_type IN ('text','markdown','html')),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    edited_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ticket_messages_ticket ON ticket_messages(ticket_id, id ASC);
CREATE INDEX IF NOT EXISTS idx_ticket_messages_public ON ticket_messages(ticket_id, id ASC) WHERE internal = FALSE;

CREATE TABLE IF NOT EXISTS ticket_attachments (
    id BIGSERIAL PRIMARY KEY,
    -- NULL = 已上传但尚未归属工单（提单表单先传附件、后建单），建单/回复时回填
    ticket_id BIGINT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    message_id BIGINT NULL REFERENCES ticket_messages(id) ON DELETE CASCADE,
    file_name VARCHAR(256) NOT NULL,
    content_type VARCHAR(128) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    -- 与平台横幅一致的规范引用：storage://{configID}/{objectKey}
    storage_ref TEXT NOT NULL,
    uploaded_by_type VARCHAR(8) NOT NULL DEFAULT 'admin' CHECK (uploaded_by_type IN ('user','admin')),
    uploaded_by_id BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ticket_attachments_ticket ON ticket_attachments(ticket_id, id ASC);
CREATE INDEX IF NOT EXISTS idx_ticket_attachments_message ON ticket_attachments(message_id);

-- 工单时间线：一切状态变更的不可变流水，供审计与详情页时间轴
CREATE TABLE IF NOT EXISTS ticket_events (
    id BIGSERIAL PRIMARY KEY,
    ticket_id BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    event VARCHAR(48) NOT NULL,     -- created / assigned / status_changed / priority_changed / replied / rated / ...
    actor_type VARCHAR(16) NOT NULL DEFAULT 'system' CHECK (actor_type IN ('user','admin','system')),
    actor_id BIGINT NULL,
    actor_name VARCHAR(128) NOT NULL DEFAULT '',
    from_value TEXT NOT NULL DEFAULT '',
    to_value TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ticket_events_ticket ON ticket_events(ticket_id, id ASC);

-- 关注人：非受理人但需要接收通知的管理员
CREATE TABLE IF NOT EXISTS ticket_watchers (
    ticket_id BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (ticket_id, admin_id)
);

CREATE INDEX IF NOT EXISTS idx_ticket_watchers_admin ON ticket_watchers(admin_id);

-- 快捷回复模板
CREATE TABLE IF NOT EXISTS ticket_quick_replies (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL DEFAULT 0,
    title VARCHAR(128) NOT NULL,
    content TEXT NOT NULL,
    category_id BIGINT NULL REFERENCES ticket_categories(id) ON DELETE SET NULL,
    -- 仅创建者可见的私人话术；NULL = 全员共享
    owner_admin_id BIGINT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    usage_count BIGINT NOT NULL DEFAULT 0,
    sort INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ticket_quick_replies_app ON ticket_quick_replies(appid, sort ASC);

-- ─────────────────────────── 统一通知出口 ───────────────────────────

-- 渠道实例：一行 = 一个可投递的目标（某个飞书群机器人 / 某个 Webhook / 邮件出口 ...）
CREATE TABLE IF NOT EXISTS notify_channels (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL DEFAULT 0,               -- 0 = 平台级渠道
    key VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    -- 渠道类型，决定用哪个 provider 渲染与投递
    kind VARCHAR(32) NOT NULL
        CHECK (kind IN ('feishu_bot','feishu_app','dingtalk_bot','wecom_bot','slack_webhook','webhook','email','inapp','realtime')),
    -- 非敏感配置（webhook url、chat id、msg 类型、邮件收件人 ...）
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- 敏感配置（签名密钥、app_secret）AES-GCM 密文，密钥派生自 SECURITY_MASTER_KEY
    secret_cipher TEXT NOT NULL DEFAULT '',
    -- 出网响应只回传是否已配置 + 尾部提示
    secret_hint VARCHAR(32) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    -- 限流：每分钟最多投递条数，0 = 不限
    rate_limit_per_minute INTEGER NOT NULL DEFAULT 0,
    -- 最近一次投递自检结果
    last_status VARCHAR(16) NOT NULL DEFAULT '',   -- success / failed
    last_error TEXT NOT NULL DEFAULT '',
    last_sent_at TIMESTAMPTZ NULL,
    created_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_notify_channels_key UNIQUE (appid, key)
);

CREATE INDEX IF NOT EXISTS idx_notify_channels_kind ON notify_channels(kind, enabled);

-- 事件路由：某个事件 key 命中条件后要投递到哪些渠道
CREATE TABLE IF NOT EXISTS notify_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES notify_channels(id) ON DELETE CASCADE,
    -- 事件 key，支持尾部通配：ticket.* / ticket.created / *
    event_key VARCHAR(96) NOT NULL,
    appid BIGINT NULL,                             -- NULL = 不限应用
    -- 过滤条件：只有命中才投递
    min_priority VARCHAR(16) NULL,                 -- low/normal/high/urgent，低于该级别不投
    category_ids BIGINT[] NOT NULL DEFAULT ARRAY[]::BIGINT[],
    -- 模板覆盖：为空时用 notify_templates 里的默认模板
    template_id BIGINT NULL,
    -- 静默窗口（避免夜间打扰），形如 {"timezone":"Asia/Shanghai","start":"23:00","end":"08:00"}
    quiet_hours JSONB NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notify_subscriptions_event ON notify_subscriptions(event_key, enabled);
CREATE INDEX IF NOT EXISTS idx_notify_subscriptions_channel ON notify_subscriptions(channel_id);

-- 模板：按 事件 key + 渠道类型 渲染标题/正文，支持 {{.Var}} 占位
CREATE TABLE IF NOT EXISTS notify_templates (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL DEFAULT 0,
    key VARCHAR(96) NOT NULL,
    name VARCHAR(128) NOT NULL,
    event_key VARCHAR(96) NOT NULL,
    -- 为空表示适用于所有渠道类型
    channel_kind VARCHAR(32) NOT NULL DEFAULT '',
    title_template TEXT NOT NULL DEFAULT '',
    body_template TEXT NOT NULL DEFAULT '',
    -- 飞书/钉钉卡片的结构化覆盖（为空则由 provider 自动构建）
    card_template JSONB NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_notify_templates_key UNIQUE (appid, key)
);

CREATE INDEX IF NOT EXISTS idx_notify_templates_event ON notify_templates(event_key, channel_kind);

-- 投递留痕：每一次尝试一行，便于排障与重放
CREATE TABLE IF NOT EXISTS notify_deliveries (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NULL REFERENCES notify_channels(id) ON DELETE SET NULL,
    channel_kind VARCHAR(32) NOT NULL DEFAULT '',
    event_key VARCHAR(96) NOT NULL,
    appid BIGINT NOT NULL DEFAULT 0,
    -- 关联业务对象（工单 ID 等），便于详情页回查
    resource VARCHAR(32) NOT NULL DEFAULT '',
    resource_id VARCHAR(64) NOT NULL DEFAULT '',
    -- 幂等键：同一事件对同一渠道只投一次
    dedupe_key VARCHAR(128) NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','success','failed','skipped','dropped')),
    attempt INTEGER NOT NULL DEFAULT 0,
    request_snippet TEXT NOT NULL DEFAULT '',
    response_snippet TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_notify_deliveries_time ON notify_deliveries(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notify_deliveries_channel ON notify_deliveries(channel_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notify_deliveries_resource ON notify_deliveries(resource, resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_notify_deliveries_dedupe
    ON notify_deliveries(dedupe_key) WHERE dedupe_key IS NOT NULL;

-- ─────────────────────────── 内置种子数据 ───────────────────────────

-- 平台级默认 SLA：新装即可用，工单不至于没有时限
INSERT INTO ticket_sla_policies (appid, name, description)
SELECT 0, '默认 SLA', '平台内置：紧急 30 分钟首响 / 4 小时解决'
WHERE NOT EXISTS (SELECT 1 FROM ticket_sla_policies WHERE appid = 0 AND name = '默认 SLA');

-- 平台级默认分类
INSERT INTO ticket_categories (appid, key, name, description, default_priority, sort)
SELECT 0, v.key, v.name, v.description, v.priority, v.sort
FROM (VALUES
    ('account',   '账号问题', '登录失败、找回密码、账号被封等', 'high',   10),
    ('payment',   '支付问题', '充值未到账、退款、订单异常',     'urgent', 20),
    ('bug',       '功能异常', '功能报错、页面白屏、数据错乱',   'normal', 30),
    ('suggestion','产品建议', '功能建议与体验反馈',             'low',    40),
    ('other',     '其它',     '未归类问题',                     'normal', 90)
) AS v(key, name, description, priority, sort)
WHERE NOT EXISTS (SELECT 1 FROM ticket_categories c WHERE c.appid = 0 AND c.key = v.key);

-- 把默认 SLA 绑定到平台级分类
UPDATE ticket_categories c
SET sla_policy_id = p.id
FROM ticket_sla_policies p
WHERE c.appid = 0 AND c.sla_policy_id IS NULL AND p.appid = 0 AND p.name = '默认 SLA';

-- 平台级默认处理组
INSERT INTO ticket_groups (appid, key, name, description, assign_strategy)
SELECT 0, 'platform_support', '平台支持组', '平台内置：承接未指定处理组的工单', 'least_open'
WHERE NOT EXISTS (SELECT 1 FROM ticket_groups WHERE appid = 0 AND key = 'platform_support');
