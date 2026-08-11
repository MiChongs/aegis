DROP INDEX IF EXISTS idx_device_names_manufacturer;

ALTER TABLE device_marketing_names
    DROP COLUMN IF EXISTS device_image_url,
    DROP COLUMN IF EXISTS manufacturer_icon_url,
    DROP COLUMN IF EXISTS manufacturer;
