-- 接入协议重构：把「强制 Transport 加密 + 允许旧接口」两个正交开关，
-- 换成一个单调的安全等级枚举，接入方只需要认识一个词。
--
--   standard —— HTTPS + X-Aegis-App-Key 头，纯 JSON（默认）
--   signed   —— 额外要求 X-Aegis-Signature（HMAC-SHA256(appSecret, ...)）
--   sealed   —— 在 signed 之上再要求 Aegis Transport v2 端到端加密载荷
--
-- 三档共用同一批 /api/v1/apps/{appKey}/* 路径与同一份 JSON 结构，
-- 等级只决定请求"怎么包装"，因此 SDK 只有一层可替换的 transport 适配器。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此本文件每条语句都必须可重复执行。

ALTER TABLE app_auth_protocol_policies
    ADD COLUMN IF NOT EXISTS security_level VARCHAR(16) NOT NULL DEFAULT 'standard',
    ADD COLUMN IF NOT EXISTS signing_secret_cipher TEXT NULL,
    ADD COLUMN IF NOT EXISTS signing_secret_hint VARCHAR(32) NULL,
    ADD COLUMN IF NOT EXISTS signing_secret_rotated_at TIMESTAMPTZ NULL;

-- 存量策略若开过强制加密，等价迁移到 sealed，避免降级放开防护。
-- 第二次执行时 transport_required 已被下面 DROP 掉，故整段跳过。
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'app_auth_protocol_policies'
          AND column_name = 'transport_required'
    ) THEN
        EXECUTE 'UPDATE app_auth_protocol_policies
                 SET security_level = ''sealed'' WHERE transport_required = true';
    END IF;
END $$;

-- transport_required 的语义已被 security_level='sealed' 完整覆盖；
-- 互锁 CHECK（allow_legacy OR transport_required）随之失去意义：
-- allow_legacy 现在只表示"旧 /api/auth/* 明文命名空间是否仍可用"，与新协议等级正交。
ALTER TABLE app_auth_protocol_policies
    DROP CONSTRAINT IF EXISTS ck_app_auth_protocol_transport;

ALTER TABLE app_auth_protocol_policies
    DROP COLUMN IF EXISTS transport_required;

-- 与 000059 同一写法：先 DROP IF EXISTS，否则第二次执行报 42710。
ALTER TABLE app_auth_protocol_policies
    DROP CONSTRAINT IF EXISTS ck_app_auth_protocol_security_level;
ALTER TABLE app_auth_protocol_policies
    ADD CONSTRAINT ck_app_auth_protocol_security_level
        CHECK (security_level IN ('standard', 'signed', 'sealed'));

-- signed / sealed 需要一把可轮换的应用密钥；密文与提示必须成对出现。
ALTER TABLE app_auth_protocol_policies
    DROP CONSTRAINT IF EXISTS ck_app_auth_protocol_signing_secret;
ALTER TABLE app_auth_protocol_policies
    ADD CONSTRAINT ck_app_auth_protocol_signing_secret
        CHECK (
            (signing_secret_cipher IS NULL AND signing_secret_hint IS NULL)
            OR (length(signing_secret_cipher) >= 32 AND signing_secret_hint IS NOT NULL)
        );

CREATE INDEX IF NOT EXISTS idx_app_auth_protocol_security_level
    ON app_auth_protocol_policies (security_level);
