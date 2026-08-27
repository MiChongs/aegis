-- ── AI 供应商通道与 Agent 会话 ──
--
-- 平台接入大模型供应商，首个消费方是「远程函数写作助手」：
-- 控制台函数编辑器里的 Agent、脚本沙箱里的 aegis.ai.chat()、
-- 以及面向接入方后端的 OpenAI / Anthropic 兼容网关，全部经由这套配置取通道。
--
-- 作用域沿用邮件通道那一套：appid IS NULL 表示平台级（Go 侧以 0 表示），
-- NULL 而不是 0 是为了保住指向 apps(id) 的外键 —— 删应用时要连带清走它的配置。

CREATE TABLE IF NOT EXISTS ai_provider_configs (
    id          BIGSERIAL PRIMARY KEY,
    appid       BIGINT REFERENCES apps(id) ON DELETE CASCADE,
    name        VARCHAR(64)  NOT NULL,
    provider    VARCHAR(32)  NOT NULL,
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    is_default  BOOLEAN      NOT NULL DEFAULT FALSE,
    -- shared 只对平台级配置有意义：允许应用在自己没有任何可用配置时回落到这条通道。
    -- 打开它意味着应用的调用花的是平台的钱，必须是平台管理员的显式授权。
    shared      BOOLEAN      NOT NULL DEFAULT FALSE,
    -- priority 供应商链路里的次序，小的先试 —— 「完整的供应商链路」就是按它排的队。
    priority    INT          NOT NULL DEFAULT 0,
    description TEXT         NOT NULL DEFAULT '',
    -- 字段值放通用袋子（键的含义由 Go 侧供应商目录声明），密钥单独一袋、AES-GCM 加密。
    settings    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    secrets     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_ai_provider_configs_shared CHECK (shared = FALSE OR appid IS NULL)
);

-- 同一作用域下配置名唯一。COALESCE 是因为两个 NULL 在 Postgres 里互不相等，
-- 不这样写能建出两条同名的平台级配置，按名取配置就会随机命中。
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_provider_configs_scope_name
    ON ai_provider_configs (COALESCE(appid, 0), name);

-- 每个作用域至多一条默认配置，与邮件通道同一条教训：两条默认并存时
-- 「改了配置没生效」只能靠 ORDER BY 的偶然顺序解释。
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_provider_configs_scope_default
    ON ai_provider_configs (COALESCE(appid, 0))
    WHERE is_default;

-- 链路解析的两条常用检索路径。
CREATE INDEX IF NOT EXISTS idx_ai_provider_configs_app_chain
    ON ai_provider_configs (appid, priority, id)
    WHERE enabled;

CREATE INDEX IF NOT EXISTS idx_ai_provider_configs_shared_chain
    ON ai_provider_configs (priority, id)
    WHERE appid IS NULL AND shared AND enabled;

-- ── 技能（可复用提示词包）──
--
-- 内置技能在 Go 侧目录里，这张表只放自定义的。作用域同上。
CREATE TABLE IF NOT EXISTS ai_skills (
    id          BIGSERIAL PRIMARY KEY,
    appid       BIGINT REFERENCES apps(id) ON DELETE CASCADE,
    key         VARCHAR(64)  NOT NULL,
    name        VARCHAR(128) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    content     TEXT         NOT NULL DEFAULT '',
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_skills_scope_key
    ON ai_skills (COALESCE(appid, 0), key);

-- ── MCP 服务器 ──
--
-- headers_cipher 整体加密：MCP 的鉴权通常放在自定义头里，逐键区分哪个算密钥
-- 只会让人漏标。
CREATE TABLE IF NOT EXISTS ai_mcp_servers (
    id             BIGSERIAL PRIMARY KEY,
    appid          BIGINT REFERENCES apps(id) ON DELETE CASCADE,
    name           VARCHAR(64)  NOT NULL,
    url            TEXT         NOT NULL,
    enabled        BOOLEAN      NOT NULL DEFAULT TRUE,
    description    TEXT         NOT NULL DEFAULT '',
    headers_cipher TEXT         NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_mcp_servers_scope_name
    ON ai_mcp_servers (COALESCE(appid, 0), name);

-- ── Agent 会话与消息 ──
--
-- compact_summary / compacted_before 支撑自动压缩：正文超过阈值时把旧消息
-- 摘要成一段滚动总结，水位线之前的消息不再送模型，但仍留在库里供界面回放。
CREATE TABLE IF NOT EXISTS ai_conversations (
    id                 BIGSERIAL PRIMARY KEY,
    appid              BIGINT      NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    admin_id           BIGINT      NOT NULL,
    scene              VARCHAR(32) NOT NULL DEFAULT 'function',
    ref                VARCHAR(128) NOT NULL DEFAULT '',
    title              VARCHAR(200) NOT NULL DEFAULT '',
    provider_config_id BIGINT      NOT NULL DEFAULT 0,
    model              VARCHAR(128) NOT NULL DEFAULT '',
    input_tokens       BIGINT      NOT NULL DEFAULT 0,
    output_tokens      BIGINT      NOT NULL DEFAULT 0,
    compact_summary    TEXT        NOT NULL DEFAULT '',
    compacted_before   BIGINT      NOT NULL DEFAULT 0,
    compactions        INT         NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 会话列表按「这个管理员在这个函数下聊过什么」检索，新的在前。
CREATE INDEX IF NOT EXISTS idx_ai_conversations_anchor
    ON ai_conversations (appid, admin_id, scene, ref, updated_at DESC);

-- parts 存界面分片（text / reasoning / tool-xxx），回放时原样下发；
-- 喂给模型前由服务层翻译。存界面格式是因为工具调用的输入输出界面要逐条展示，
-- 而模型格式在不同供应商之间来回转换会丢字段。
CREATE TABLE IF NOT EXISTS ai_messages (
    id              BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT      NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    role            VARCHAR(16) NOT NULL,
    parts           JSONB       NOT NULL DEFAULT '[]'::jsonb,
    usage           JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_messages_conversation
    ON ai_messages (conversation_id, id);
