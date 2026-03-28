-- 抽奖系统：活动、奖品、参与、抽奖记录、种子承诺

CREATE TABLE IF NOT EXISTS lottery_activities (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    ui_mode VARCHAR(16) NOT NULL DEFAULT 'wheel',
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    join_mode VARCHAR(16) NOT NULL DEFAULT 'manual',
    auto_join_rules JSONB NOT NULL DEFAULT '{}'::jsonb,
    cost_type VARCHAR(16) NOT NULL DEFAULT 'free',
    cost_amount INT NOT NULL DEFAULT 0,
    daily_limit INT NOT NULL DEFAULT 0,
    total_limit INT NOT NULL DEFAULT 0,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    seed_hash VARCHAR(128),
    seed_value VARCHAR(128),
    chain_tx_hash VARCHAR(128),
    chain_network VARCHAR(32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_lottery_activities_appid_status ON lottery_activities(appid, status);
CREATE INDEX IF NOT EXISTS idx_lottery_activities_appid_time ON lottery_activities(appid, start_time, end_time);

CREATE TABLE IF NOT EXISTS lottery_prizes (
    id BIGSERIAL PRIMARY KEY,
    activity_id BIGINT NOT NULL REFERENCES lottery_activities(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32) NOT NULL,
    value VARCHAR(255) NOT NULL DEFAULT '',
    image_url TEXT,
    quantity INT NOT NULL DEFAULT -1,
    used INT NOT NULL DEFAULT 0,
    weight INT NOT NULL DEFAULT 1,
    position INT NOT NULL DEFAULT 0,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    extra JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_lottery_prizes_activity ON lottery_prizes(activity_id, position);

CREATE TABLE IF NOT EXISTS lottery_participants (
    id BIGSERIAL PRIMARY KEY,
    activity_id BIGINT NOT NULL REFERENCES lottery_activities(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    join_type VARCHAR(16) NOT NULL DEFAULT 'manual',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_lottery_participant UNIQUE (activity_id, user_id)
);

CREATE TABLE IF NOT EXISTS lottery_draws (
    id BIGSERIAL PRIMARY KEY,
    activity_id BIGINT NOT NULL REFERENCES lottery_activities(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    prize_id BIGINT REFERENCES lottery_prizes(id),
    prize_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    draw_seed VARCHAR(255),
    draw_proof VARCHAR(512),
    status VARCHAR(16) NOT NULL DEFAULT 'awarded',
    claimed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_lottery_draws_activity_user ON lottery_draws(activity_id, user_id);
CREATE INDEX IF NOT EXISTS idx_lottery_draws_user ON lottery_draws(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS lottery_seed_commitments (
    id BIGSERIAL PRIMARY KEY,
    activity_id BIGINT NOT NULL REFERENCES lottery_activities(id) ON DELETE CASCADE,
    round INT NOT NULL DEFAULT 1,
    seed_hash VARCHAR(128) NOT NULL,
    seed_value VARCHAR(128),
    committed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revealed_at TIMESTAMPTZ,
    chain_tx_hash VARCHAR(128),
    CONSTRAINT uq_lottery_seed_round UNIQUE (activity_id, round)
);
