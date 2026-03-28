CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_users_app_created_desc
    ON users (appid, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_users_app_enabled_created_desc
    ON users (appid, enabled, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_users_account_trgm
    ON users USING gin (account gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_user_profiles_nickname_trgm
    ON user_profiles USING gin (nickname gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_user_profiles_email_trgm
    ON user_profiles USING gin (email gin_trgm_ops);
