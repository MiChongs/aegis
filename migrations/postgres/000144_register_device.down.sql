-- +migrate Down
DROP INDEX IF EXISTS idx_register_ip_events_device;
DROP INDEX IF EXISTS idx_register_ip_ledger_device_all;
DROP INDEX IF EXISTS idx_register_ip_ledger_device;
DROP TABLE IF EXISTS register_device_lists;
