-- ── 平台治理：超级管理员 / 平台管理员对全站应用的高级管控 ──
--
-- 与 apps.status / login_status / register_status 的关系：
--   那三个开关是**应用自治**的营业开关，应用管理员自己就能开关；
--   本表是**平台强制**的治理状态，只有平台级管理员能改，且优先级更高 ——
--   被冻结的应用即使 apps.status = true 也一律拒绝服务。
--   两者分表存放，正是为了让应用管理员改不动治理结论（他只能改 apps 那三列）。
CREATE TABLE IF NOT EXISTS app_governance_states (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    -- active / restricted / frozen / suspended / banned / archived
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    reason TEXT NOT NULL DEFAULT '',
    -- 细粒度限制项，形如 {"blockLogin":true,"blockRegister":true,...}
    -- 每一项都必须有代码执行点，只落库不生效的开关比没有更危险
    restrictions JSONB NOT NULL DEFAULT '{}'::jsonb,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    start_at TIMESTAMPTZ,
    -- 到期自动恢复；banned / archived 恒为 NULL（永久，需人工解除）
    end_at TIMESTAMPTZ,
    operator_admin_id BIGINT REFERENCES admin_accounts(id) ON DELETE SET NULL,
    operator_name VARCHAR(128) NOT NULL DEFAULT '',
    last_action VARCHAR(32) NOT NULL DEFAULT '',
    -- none / pending / approved / rejected
    appeal_status VARCHAR(16) NOT NULL DEFAULT 'none',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 到期扫描按 (state, end_at) 走索引；active 行不参与扫描故用部分索引
CREATE INDEX IF NOT EXISTS idx_app_governance_states_due
    ON app_governance_states(end_at)
    WHERE state <> 'active' AND end_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_app_governance_states_state
    ON app_governance_states(state);

-- 治理动作流水：只增不改，任何一次冻结 / 封禁 / 解除都留痕。
-- 状态表只有"现在是什么"，追责要靠这张表回答"谁在什么时候基于什么把它变成这样"。
CREATE TABLE IF NOT EXISTS app_governance_actions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- restrict / freeze / suspend / ban / archive / restore / update / expire / appeal_approved
    action VARCHAR(32) NOT NULL,
    from_state VARCHAR(16) NOT NULL DEFAULT 'active',
    to_state VARCHAR(16) NOT NULL DEFAULT 'active',
    reason TEXT NOT NULL DEFAULT '',
    restrictions JSONB NOT NULL DEFAULT '{}'::jsonb,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    end_at TIMESTAMPTZ,
    -- 操作者为 NULL 表示系统自动（到期恢复）
    operator_admin_id BIGINT REFERENCES admin_accounts(id) ON DELETE SET NULL,
    operator_name VARCHAR(128) NOT NULL DEFAULT '',
    operator_ip VARCHAR(64) NOT NULL DEFAULT '',
    revoked_sessions INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_app_governance_actions_app
    ON app_governance_actions(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_governance_actions_created
    ON app_governance_actions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_governance_actions_action
    ON app_governance_actions(action, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_governance_actions_operator
    ON app_governance_actions(operator_admin_id, created_at DESC);

-- 申诉：被治理应用的管理员在这里陈情，平台管理员在这里裁决。
-- 没有这条通道，误伤一个应用就等于把它的运营方彻底挡在门外。
CREATE TABLE IF NOT EXISTS app_governance_appeals (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    action_id BIGINT REFERENCES app_governance_actions(id) ON DELETE SET NULL,
    state_snapshot VARCHAR(16) NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    attachments JSONB NOT NULL DEFAULT '[]'::jsonb,
    submitted_by_admin_id BIGINT REFERENCES admin_accounts(id) ON DELETE SET NULL,
    submitted_by_name VARCHAR(128) NOT NULL DEFAULT '',
    -- pending / approved / rejected / withdrawn
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    review_admin_id BIGINT REFERENCES admin_accounts(id) ON DELETE SET NULL,
    review_admin_name VARCHAR(128) NOT NULL DEFAULT '',
    review_note TEXT NOT NULL DEFAULT '',
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 同一应用同时只允许一份待审申诉，避免刷单式申诉淹没审核队列
CREATE UNIQUE INDEX IF NOT EXISTS uq_app_governance_appeals_pending
    ON app_governance_appeals(appid)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_app_governance_appeals_status
    ON app_governance_appeals(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_governance_appeals_app
    ON app_governance_appeals(appid, created_at DESC);
