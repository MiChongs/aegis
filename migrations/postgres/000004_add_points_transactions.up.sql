-- +migrate Up
CREATE TABLE IF NOT EXISTS integral_transactions (
    id BIGSERIAL PRIMARY KEY,
    transaction_no VARCHAR(40) NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    type VARCHAR(32) NOT NULL,
    category VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL,
    balance_before BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'completed',
    title VARCHAR(200) NOT NULL,
    description TEXT NULL,
    source_id BIGINT NULL,
    source_type VARCHAR(64) NULL,
    multiplier NUMERIC(4,2) NOT NULL DEFAULT 1.00,
    client_ip VARCHAR(64) NULL,
    user_agent TEXT NULL,
    extra_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_integral_transactions_no UNIQUE (transaction_no)
);

CREATE TABLE IF NOT EXISTS experience_transactions (
    id BIGSERIAL PRIMARY KEY,
    transaction_no VARCHAR(40) NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    type VARCHAR(32) NOT NULL,
    category VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL,
    balance_before BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    level_before INTEGER NOT NULL DEFAULT 1,
    level_after INTEGER NOT NULL DEFAULT 1,
    status VARCHAR(32) NOT NULL DEFAULT 'completed',
    title VARCHAR(200) NOT NULL,
    description TEXT NULL,
    source_id BIGINT NULL,
    source_type VARCHAR(64) NULL,
    multiplier NUMERIC(4,2) NOT NULL DEFAULT 1.00,
    is_level_up BOOLEAN NOT NULL DEFAULT FALSE,
    client_ip VARCHAR(64) NULL,
    user_agent TEXT NULL,
    extra_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_experience_transactions_no UNIQUE (transaction_no)
);

CREATE INDEX IF NOT EXISTS idx_integral_transactions_user_time ON integral_transactions(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_integral_transactions_app_time ON integral_transactions(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_integral_transactions_category ON integral_transactions(category);
CREATE INDEX IF NOT EXISTS idx_experience_transactions_user_time ON experience_transactions(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_experience_transactions_app_time ON experience_transactions(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_experience_transactions_category ON experience_transactions(category);
