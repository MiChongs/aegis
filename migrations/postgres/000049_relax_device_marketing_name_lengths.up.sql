-- 设备营销名称字段长度放宽
-- 种子数据中存在 Android 设备的 identifier / marketing_name 长度超过 256 字符（ SQLSTATE 22001 ）
-- 例如某些联想 ThinkPad / 厂商 OEM 机型带完整产品线描述
-- 统一改为 TEXT，PostgreSQL 中 TEXT 与 VARCHAR 存储性能无差别但无长度限制

ALTER TABLE device_marketing_names
    ALTER COLUMN identifier     TYPE TEXT,
    ALTER COLUMN marketing_name TYPE TEXT;

-- 唯一约束和索引会自动继承类型变更，无需重建
