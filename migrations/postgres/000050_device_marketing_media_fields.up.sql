-- 设备营销名称扩展：厂商 + 厂商图标 + 设备展示图
-- 用于前端审计 / session 展示时渲染真实设备外观和品牌标识
ALTER TABLE device_marketing_names
    ADD COLUMN IF NOT EXISTS manufacturer          TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS manufacturer_icon_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS device_image_url      TEXT NOT NULL DEFAULT '';

-- 按厂商筛选的索引（LOWER 避免大小写差异）
CREATE INDEX IF NOT EXISTS idx_device_names_manufacturer
    ON device_marketing_names(LOWER(manufacturer));
