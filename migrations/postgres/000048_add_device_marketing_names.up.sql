-- 设备营销名称字典
-- 数据来源：kotlin 开源库 de.boehrsi.devicemarketingnames（DeviceIdentifiers.kt）
-- 用途：把 iOS 的 machine identifier（如 iPhone14,3）和 Android 的 Build.MODEL（如 SM-G998B）
--       映射为人类可读营销名称（如 "iPhone 13 Pro Max" / "Galaxy S21 Ultra 5G"），供日志/审计/session 展示使用

CREATE TABLE IF NOT EXISTS device_marketing_names (
    id             BIGSERIAL     PRIMARY KEY,
    platform       VARCHAR(16)   NOT NULL,                     -- ios / android
    identifier     TEXT          NOT NULL,                     -- 原始识别码（大小写敏感，部分 OEM 机型可能超过 256 字符）
    marketing_name TEXT          NOT NULL,                     -- 人类可读名（同样可能超长）
    source         VARCHAR(32)   NOT NULL DEFAULT 'seed',      -- seed / manual / import
    notes          TEXT          NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT uniq_device_platform_identifier UNIQUE (platform, identifier)
);

CREATE INDEX IF NOT EXISTS idx_device_names_platform    ON device_marketing_names(platform, marketing_name);
CREATE INDEX IF NOT EXISTS idx_device_names_name_lower  ON device_marketing_names(LOWER(marketing_name));
CREATE INDEX IF NOT EXISTS idx_device_names_identifier  ON device_marketing_names(platform, LOWER(identifier));
