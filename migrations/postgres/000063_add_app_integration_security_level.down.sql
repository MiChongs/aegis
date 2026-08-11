ALTER TABLE app_auth_protocol_policies
    ADD COLUMN IF NOT EXISTS transport_required BOOLEAN NOT NULL DEFAULT false;

-- 回滚回旧语义：sealed 等价于「强制 Transport v2」
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'app_auth_protocol_policies'
          AND column_name = 'security_level'
    ) THEN
        EXECUTE 'UPDATE app_auth_protocol_policies
                 SET transport_required = true WHERE security_level = ''sealed''';
    END IF;
END $$;

ALTER TABLE app_auth_protocol_policies
    DROP CONSTRAINT IF EXISTS ck_app_auth_protocol_signing_secret,
    DROP CONSTRAINT IF EXISTS ck_app_auth_protocol_security_level;

DROP INDEX IF EXISTS idx_app_auth_protocol_security_level;

ALTER TABLE app_auth_protocol_policies
    DROP COLUMN IF EXISTS signing_secret_rotated_at,
    DROP COLUMN IF EXISTS signing_secret_hint,
    DROP COLUMN IF EXISTS signing_secret_cipher,
    DROP COLUMN IF EXISTS security_level;

-- 旧约束要求「禁用旧协议时必须强制 Transport v2」，先把违反者拉回合法状态
UPDATE app_auth_protocol_policies
SET allow_legacy = true
WHERE allow_legacy = false AND transport_required = false;

ALTER TABLE app_auth_protocol_policies
    DROP CONSTRAINT IF EXISTS ck_app_auth_protocol_transport;
ALTER TABLE app_auth_protocol_policies
    ADD CONSTRAINT ck_app_auth_protocol_transport
        CHECK (allow_legacy OR transport_required);
