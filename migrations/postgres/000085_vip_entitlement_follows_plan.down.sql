-- +migrate Down

ALTER TABLE vip_transactions
    DROP COLUMN IF EXISTS revoke_reason,
    DROP COLUMN IF EXISTS revoked_at,
    DROP COLUMN IF EXISTS active_until,
    DROP COLUMN IF EXISTS active_from;
