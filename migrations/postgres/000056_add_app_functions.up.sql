CREATE TABLE IF NOT EXISTS app_functions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(96) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    runtime VARCHAR(16) NOT NULL CHECK (runtime IN ('wasm', 'http')),
    status VARCHAR(16) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'disabled')),
    active_version VARCHAR(64) NOT NULL DEFAULT '',
    capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    timeout_ms INTEGER NOT NULL DEFAULT 500 CHECK (timeout_ms BETWEEN 10 AND 30000),
    max_request_bytes INTEGER NOT NULL DEFAULT 65536 CHECK (max_request_bytes BETWEEN 1 AND 1048576),
    max_response_bytes INTEGER NOT NULL DEFAULT 65536 CHECK (max_response_bytes BETWEEN 1 AND 1048576),
    created_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (appid, name)
);

CREATE TABLE IF NOT EXISTS app_function_versions (
    id BIGSERIAL PRIMARY KEY,
    function_id BIGINT NOT NULL REFERENCES app_functions(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    version VARCHAR(64) NOT NULL,
    endpoint_url TEXT NOT NULL DEFAULT '',
    response_public_key TEXT NOT NULL DEFAULT '',
    wasm_module BYTEA NULL,
    artifact_sha256 CHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'staged' CHECK (status IN ('staged', 'active', 'retired')),
    created_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ NULL,
    UNIQUE (function_id, version),
    CHECK (
        (endpoint_url <> '' AND wasm_module IS NULL)
        OR (endpoint_url = '' AND wasm_module IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS app_function_keys (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(96) NOT NULL,
    key_prefix VARCHAR(16) NOT NULL,
    key_hash BYTEA NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_by BIGINT NULL,
    last_used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ NULL,
    UNIQUE (appid, name),
    UNIQUE (appid, key_prefix),
    UNIQUE (key_hash)
);

CREATE TABLE IF NOT EXISTS app_function_invocations (
    id BIGSERIAL PRIMARY KEY,
    event_id UUID NOT NULL,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    function_id BIGINT NOT NULL REFERENCES app_functions(id) ON DELETE CASCADE,
    version_id BIGINT NOT NULL REFERENCES app_function_versions(id) ON DELETE RESTRICT,
    caller_type VARCHAR(16) NOT NULL,
    caller_id BIGINT NULL,
    status VARCHAR(16) NOT NULL,
    duration_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    request_sha256 CHAR(64) NOT NULL,
    response_sha256 CHAR(64) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    result JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (appid, event_id)
);

CREATE INDEX IF NOT EXISTS idx_app_functions_appid ON app_functions(appid, status);
CREATE INDEX IF NOT EXISTS idx_app_function_versions_lookup ON app_function_versions(appid, function_id, status);
CREATE INDEX IF NOT EXISTS idx_app_function_keys_lookup ON app_function_keys(appid, key_prefix, status);
CREATE INDEX IF NOT EXISTS idx_app_function_invocations_lookup ON app_function_invocations(appid, function_id, created_at DESC);
