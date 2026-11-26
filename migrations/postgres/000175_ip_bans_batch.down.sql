-- +migrate Down
DROP INDEX IF EXISTS idx_ip_bans_ip_created;
DROP INDEX IF EXISTS idx_ip_bans_country_status;
DROP INDEX IF EXISTS idx_ip_bans_source_status;
ALTER TABLE ip_bans
    DROP COLUMN IF EXISTS updated_by,
    DROP COLUMN IF EXISTS delay_ms,
    DROP COLUMN IF EXISTS revoke_note;
