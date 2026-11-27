-- +migrate Down
ALTER TABLE vip_tiers DROP COLUMN IF EXISTS perks;
