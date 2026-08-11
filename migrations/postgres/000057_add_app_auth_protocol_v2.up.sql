CREATE TABLE IF NOT EXISTS app_auth_protocol_policies (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    protocol_version VARCHAR(32) NOT NULL DEFAULT 'aegis-auth-v2',
    identifiers TEXT[] NOT NULL DEFAULT ARRAY['username','email'],
    login_methods TEXT[] NOT NULL DEFAULT ARRAY['password'],
    register_methods TEXT[] NOT NULL DEFAULT ARRAY['password'],
    registration_schema JSONB NOT NULL DEFAULT '[
      {"name":"account","type":"text","required":true,"mutable":false},
      {"name":"password","type":"password","required":true,"mutable":true},
      {"name":"nickname","type":"text","required":false,"mutable":true}
    ]'::jsonb,
    require_captcha BOOLEAN NOT NULL DEFAULT false,
    auto_login_after_register BOOLEAN NOT NULL DEFAULT true,
    transport_required BOOLEAN NOT NULL DEFAULT false,
    allow_legacy BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_app_auth_protocol_transport
      CHECK (allow_legacy OR transport_required),
    CONSTRAINT ck_app_auth_protocol_schema
      CHECK (jsonb_typeof(registration_schema) = 'array'
        AND jsonb_array_length(registration_schema) <= 32)
);

CREATE TABLE IF NOT EXISTS app_transport_keys (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    key_id VARCHAR(64) NOT NULL,
    algorithm VARCHAR(64) NOT NULL CHECK (algorithm = 'x25519-xchacha20-poly1305'),
    public_key BYTEA NOT NULL,
    private_key_cipher TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active','retiring','revoked')),
    not_before TIMESTAMPTZ NOT NULL,
    not_after TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ NULL,
    UNIQUE (appid, key_id),
    CONSTRAINT ck_app_transport_public_key_length CHECK (octet_length(public_key) = 32),
    CONSTRAINT ck_app_transport_private_key_cipher CHECK (length(private_key_cipher) >= 32),
    CONSTRAINT ck_app_transport_key_window CHECK (not_after > not_before)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_app_transport_active_key
ON app_transport_keys(appid) WHERE status='active';
CREATE INDEX IF NOT EXISTS idx_app_transport_key_window
ON app_transport_keys(appid, status, not_before, not_after);
