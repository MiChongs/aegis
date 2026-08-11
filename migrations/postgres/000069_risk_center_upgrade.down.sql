ALTER TABLE risk_actions DROP CONSTRAINT IF EXISTS chk_risk_actions_score_range;

DROP INDEX IF EXISTS idx_ip_risk_score;
DROP INDEX IF EXISTS idx_ip_risk_last_seen;
ALTER TABLE ip_risk_records DROP COLUMN IF EXISTS note;
ALTER TABLE ip_risk_records DROP COLUMN IF EXISTS source;
ALTER TABLE ip_risk_records DROP COLUMN IF EXISTS asn;

DROP INDEX IF EXISTS idx_device_fingerprints_last_seen;
ALTER TABLE device_fingerprints DROP COLUMN IF EXISTS note;
ALTER TABLE device_fingerprints DROP COLUMN IF EXISTS user_agent;
ALTER TABLE device_fingerprints DROP COLUMN IF EXISTS last_ip;

DROP INDEX IF EXISTS idx_risk_assessments_matched;
DROP INDEX IF EXISTS idx_risk_assessments_action;
DROP INDEX IF EXISTS idx_risk_assessments_level;
DROP INDEX IF EXISTS idx_risk_assessments_account;
DROP INDEX IF EXISTS idx_risk_assessments_device;
DROP INDEX IF EXISTS idx_risk_assessments_ip;
DROP INDEX IF EXISTS idx_risk_assessments_created;
ALTER TABLE risk_assessments DROP COLUMN IF EXISTS latency_ms;
ALTER TABLE risk_assessments DROP COLUMN IF EXISTS eval_context;
ALTER TABLE risk_assessments DROP COLUMN IF EXISTS country;
ALTER TABLE risk_assessments DROP COLUMN IF EXISTS user_agent;
ALTER TABLE risk_assessments DROP COLUMN IF EXISTS account;

ALTER TABLE risk_rules DROP COLUMN IF EXISTS updated_by;
ALTER TABLE risk_rules DROP COLUMN IF EXISTS last_hit_at;
ALTER TABLE risk_rules DROP COLUMN IF EXISTS hit_count;
