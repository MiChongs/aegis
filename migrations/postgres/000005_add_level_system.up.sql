-- +migrate Up
CREATE TABLE IF NOT EXISTS user_levels (
    id BIGSERIAL PRIMARY KEY,
    level INTEGER NOT NULL,
    level_name VARCHAR(64) NOT NULL,
    experience_required BIGINT NOT NULL,
    experience_next BIGINT NULL,
    exp_multiplier NUMERIC(4,2) NOT NULL DEFAULT 1.00,
    icon VARCHAR(255) NULL,
    color VARCHAR(16) NULL DEFAULT '#2563eb',
    privileges JSONB NOT NULL DEFAULT '[]'::jsonb,
    rewards JSONB NOT NULL DEFAULT '{}'::jsonb,
    description TEXT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_levels_level UNIQUE (level)
);

CREATE TABLE IF NOT EXISTS user_level_records (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    current_level INTEGER NOT NULL DEFAULT 1,
    current_experience BIGINT NOT NULL DEFAULT 0,
    total_experience BIGINT NOT NULL DEFAULT 0,
    next_level_experience BIGINT NULL,
    level_progress NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    highest_level INTEGER NOT NULL DEFAULT 1,
    level_up_count INTEGER NOT NULL DEFAULT 0,
    last_level_up_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_level_records_user_app UNIQUE (user_id, appid)
);

CREATE INDEX IF NOT EXISTS idx_user_levels_active_sort ON user_levels(is_active, sort_order, level);
CREATE INDEX IF NOT EXISTS idx_user_level_records_app_level_exp ON user_level_records(appid, current_level DESC, total_experience DESC);
CREATE INDEX IF NOT EXISTS idx_user_level_records_app_user ON user_level_records(appid, user_id);

INSERT INTO user_levels (
    level,
    level_name,
    experience_required,
    experience_next,
    exp_multiplier,
    icon,
    color,
    privileges,
    rewards,
    description,
    is_active,
    sort_order
) VALUES
    (1,  '新手',       0,     120, 1.00, 'seedling',    '#94a3b8', '["基础签到"]'::jsonb,                        '{"badge":"welcome"}'::jsonb,                    '默认初始等级。', TRUE, 1),
    (2,  '见习',       120,   180, 1.00, 'spark',       '#64748b', '["连续签到加成预览"]'::jsonb,              '{"title":"见习认证"}'::jsonb,                   '完成基础活跃后晋升。', TRUE, 2),
    (3,  '学徒',       300,   300, 1.02, 'feather',     '#0f766e', '["签到经验加成 2%"]'::jsonb,               '{"integral_hint":20}'::jsonb,                   '进入稳定活跃阶段。', TRUE, 3),
    (4,  '进阶',       600,   400, 1.04, 'trail',       '#0d9488', '["签到经验加成 4%"]'::jsonb,               '{"unlock":"profile-frame-1"}'::jsonb,          '开始形成稳定成长曲线。', TRUE, 4),
    (5,  '精锐',       1000,  600, 1.06, 'shield',      '#0891b2', '["签到经验加成 6%"]'::jsonb,               '{"unlock":"profile-frame-2"}'::jsonb,          '具备持续活跃能力。', TRUE, 5),
    (6,  '骨干',       1600,  900, 1.08, 'crest',       '#2563eb', '["签到经验加成 8%"]'::jsonb,               '{"unlock":"highlight-name"}'::jsonb,           '进入核心活跃用户梯队。', TRUE, 6),
    (7,  '资深',       2500,  1300, 1.10, 'anchor',     '#4f46e5', '["签到经验加成 10%"]'::jsonb,              '{"unlock":"priority-badge"}'::jsonb,           '长期活跃用户认证。', TRUE, 7),
    (8,  '先锋',       3800,  1800, 1.12, 'compass',    '#7c3aed', '["签到经验加成 12%"]'::jsonb,              '{"unlock":"theme-pack-a"}'::jsonb,             '具备显著成长表现。', TRUE, 8),
    (9,  '卓越',       5600,  2200, 1.15, 'summit',     '#9333ea', '["签到经验加成 15%"]'::jsonb,              '{"unlock":"theme-pack-b"}'::jsonb,             '达到高活跃水位。', TRUE, 9),
    (10, '专家',       7800,  2200, 1.18, 'orbit',      '#c026d3', '["签到经验加成 18%"]'::jsonb,              '{"unlock":"expert-mark"}'::jsonb,              '进入专家成长阶段。', TRUE, 10),
    (11, '大师',       10000, 3000, 1.30, 'crown',      '#db2777', '["签到经验加成 30%"]'::jsonb,              '{"unlock":"master-badge"}'::jsonb,             '达到核心高阶等级。', TRUE, 11),
    (12, '宗师',       13000, 4000, 1.32, 'aurora',     '#ea580c', '["签到经验加成 32%"]'::jsonb,              '{"unlock":"master-theme"}'::jsonb,             '维持长期高频贡献。', TRUE, 12),
    (13, '传奇',       17000, 5000, 1.35, 'phoenix',    '#d97706', '["签到经验加成 35%"]'::jsonb,              '{"unlock":"legend-badge"}'::jsonb,             '进入传奇用户层级。', TRUE, 13),
    (14, '圣辉',       22000, 6000, 1.40, 'sunfire',    '#ca8a04', '["签到经验加成 40%"]'::jsonb,              '{"unlock":"golden-title"}'::jsonb,             '极高活跃度认证。', TRUE, 14),
    (15, '天穹',       28000, 7000, 1.45, 'skyforge',   '#65a30d', '["签到经验加成 45%"]'::jsonb,              '{"unlock":"sky-forge-frame"}'::jsonb,          '进入顶尖成长序列。', TRUE, 15),
    (16, '星耀',       35000, 8000, 1.50, 'starlight',  '#16a34a', '["签到经验加成 50%"]'::jsonb,              '{"unlock":"stellar-mark"}'::jsonb,             '顶尖高活跃用户。', TRUE, 16),
    (17, '永曜',       43000, 9000, 1.55, 'everglow',   '#059669', '["签到经验加成 55%"]'::jsonb,              '{"unlock":"eternal-badge"}'::jsonb,            '长期高价值活跃认证。', TRUE, 17),
    (18, '至高',       52000, NULL, 1.60, 'apex',       '#dc2626', '["签到经验加成 60%","满级标识"]'::jsonb,   '{"unlock":"apex-title","max_level":true}'::jsonb, '当前最高等级。', TRUE, 18)
ON CONFLICT (level) DO UPDATE SET
    level_name = EXCLUDED.level_name,
    experience_required = EXCLUDED.experience_required,
    experience_next = EXCLUDED.experience_next,
    exp_multiplier = EXCLUDED.exp_multiplier,
    icon = EXCLUDED.icon,
    color = EXCLUDED.color,
    privileges = EXCLUDED.privileges,
    rewards = EXCLUDED.rewards,
    description = EXCLUDED.description,
    is_active = EXCLUDED.is_active,
    sort_order = EXCLUDED.sort_order,
    updated_at = NOW();

INSERT INTO user_level_records (
    user_id,
    appid,
    current_level,
    current_experience,
    total_experience,
    next_level_experience,
    level_progress,
    highest_level,
    level_up_count,
    last_level_up_at,
    created_at,
    updated_at
)
SELECT
    u.id,
    u.appid,
    COALESCE(curr.level, 1) AS current_level,
    GREATEST(u.experience - COALESCE(curr.experience_required, 0), 0) AS current_experience,
    u.experience AS total_experience,
    CASE
        WHEN nxt.experience_required IS NULL THEN NULL
        ELSE GREATEST(nxt.experience_required - u.experience, 0)
    END AS next_level_experience,
    CASE
        WHEN nxt.experience_required IS NULL THEN 100.00
        WHEN nxt.experience_required = COALESCE(curr.experience_required, 0) THEN 0.00
        ELSE ROUND(
            LEAST(
                100.00,
                GREATEST(
                    0.00,
                    ((u.experience - COALESCE(curr.experience_required, 0))::numeric / NULLIF((nxt.experience_required - COALESCE(curr.experience_required, 0)), 0)::numeric) * 100.00
                )
            ),
            2
        )
    END AS level_progress,
    COALESCE(curr.level, 1) AS highest_level,
    0 AS level_up_count,
    NULL AS last_level_up_at,
    NOW(),
    NOW()
FROM users u
LEFT JOIN LATERAL (
    SELECT level, experience_required
    FROM user_levels
    WHERE is_active = TRUE AND experience_required <= u.experience
    ORDER BY level DESC
    LIMIT 1
) curr ON TRUE
LEFT JOIN LATERAL (
    SELECT level, experience_required
    FROM user_levels
    WHERE is_active = TRUE AND experience_required > u.experience
    ORDER BY level ASC
    LIMIT 1
) nxt ON TRUE
ON CONFLICT (user_id, appid) DO NOTHING;
