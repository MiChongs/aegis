-- +migrate Down
DROP TABLE IF EXISTS session_audit_logs;
DROP TABLE IF EXISTS login_audit_logs;
DROP TABLE IF EXISTS oauth_bindings;
DROP TABLE IF EXISTS user_settings;
DROP TABLE IF EXISTS user_profiles;
DROP TABLE IF EXISTS users;
