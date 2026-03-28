CREATE TABLE IF NOT EXISTS plugins (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    description TEXT DEFAULT '',
    type VARCHAR(16) NOT NULL DEFAULT 'expr',
    status VARCHAR(16) NOT NULL DEFAULT 'disabled',
    version VARCHAR(32) NOT NULL DEFAULT '1.0.0',
    author VARCHAR(128) DEFAULT '',
    hooks JSONB NOT NULL DEFAULT '[]'::jsonb,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    expr_script TEXT DEFAULT '',
    wasm_module_url TEXT DEFAULT '',
    wasm_hash VARCHAR(128) DEFAULT '',
    priority INT NOT NULL DEFAULT 100,
    error_message TEXT DEFAULT '',
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plugins_status ON plugins(status);
CREATE INDEX IF NOT EXISTS idx_plugins_type ON plugins(type);

CREATE TABLE IF NOT EXISTS plugin_hook_executions (
    id BIGSERIAL PRIMARY KEY,
    plugin_id BIGINT NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    plugin_name VARCHAR(128) NOT NULL,
    hook_name VARCHAR(128) NOT NULL,
    phase VARCHAR(16) NOT NULL,
    duration_ns BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'success',
    error TEXT DEFAULT '',
    input JSONB,
    output JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plugin_exec_plugin ON plugin_hook_executions(plugin_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_plugin_exec_hook ON plugin_hook_executions(hook_name, created_at DESC);
