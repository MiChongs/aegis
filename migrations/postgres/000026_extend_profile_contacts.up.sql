-- 管理员表：新增联系方式、生日、简介、手机号
ALTER TABLE admin_accounts
  ADD COLUMN IF NOT EXISTS phone    VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS birthday DATE NULL,
  ADD COLUMN IF NOT EXISTS bio      TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS contacts JSONB NOT NULL DEFAULT '[]'::jsonb;

-- 用户资料表：新增联系方式、生日、简介、手机号
ALTER TABLE user_profiles
  ADD COLUMN IF NOT EXISTS phone    VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS birthday DATE NULL,
  ADD COLUMN IF NOT EXISTS bio      TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS contacts JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN admin_accounts.contacts IS '多平台联系方式 [{platform,value,label}]';
COMMENT ON COLUMN user_profiles.contacts  IS '多平台联系方式 [{platform,value,label}]';
