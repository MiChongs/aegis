-- +migrate Down

DROP INDEX IF EXISTS idx_vip_transactions_active;

ALTER TABLE vip_transactions DROP COLUMN IF EXISTS features;
ALTER TABLE vip_plans DROP COLUMN IF EXISTS features;

DROP TABLE IF EXISTS vip_features;
