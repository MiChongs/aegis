-- +migrate Down
DROP TABLE IF EXISTS firewall_waf_exclusions;
DROP TABLE IF EXISTS firewall_rules;
DROP INDEX IF EXISTS idx_fw_logs_matches;
DROP INDEX IF EXISTS idx_fw_logs_app_key;
DROP INDEX IF EXISTS idx_fw_logs_rule_ref;
DROP INDEX IF EXISTS idx_fw_logs_ip_action_blocked;
DROP INDEX IF EXISTS idx_fw_logs_action_blocked_at;
ALTER TABLE firewall_logs
    DROP COLUMN IF EXISTS rule_name,
    DROP COLUMN IF EXISTS rule_ref,
    DROP COLUMN IF EXISTS matches,
    DROP COLUMN IF EXISTS anomaly_score,
    DROP COLUMN IF EXISTS app_key,
    DROP COLUMN IF EXISTS transport,
    DROP COLUMN IF EXISTS stage,
    DROP COLUMN IF EXISTS action;
