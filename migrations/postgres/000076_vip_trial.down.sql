-- +migrate Down

DROP TABLE IF EXISTS vip_trial_claims;

DROP INDEX IF EXISTS uq_vip_plans_active_trial;

ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS ck_vip_plans_trial_free;
ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS ck_vip_plans_kind;

ALTER TABLE vip_plans
    DROP COLUMN IF EXISTS trial_device_limited,
    DROP COLUMN IF EXISTS kind;
