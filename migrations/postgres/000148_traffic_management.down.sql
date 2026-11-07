-- +migrate Down
DROP TABLE IF EXISTS traffic_events;
DROP TABLE IF EXISTS traffic_metrics_minute;
DROP TABLE IF EXISTS traffic_revisions;
DROP TABLE IF EXISTS traffic_rules;
DROP TABLE IF EXISTS traffic_settings;
DROP TABLE IF EXISTS native_plugins;
