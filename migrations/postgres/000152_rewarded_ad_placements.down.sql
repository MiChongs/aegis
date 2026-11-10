-- +migrate Down
ALTER TABLE app_rewarded_ad_configs DROP COLUMN IF EXISTS placements;
