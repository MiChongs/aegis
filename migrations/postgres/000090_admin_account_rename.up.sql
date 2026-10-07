-- +migrate Up
-- 管理员账号自助：一次性改名与修改密码。
--
--   account_changed_at   改名时间。非空即表示唯一一次改名机会已用掉，永久不可再改
--   previous_account     改名前的账号。保留下来有两个用途：
--                          1. 旧名永久保留，不能被其他人注册或改成，免得审计日志里同一个名字指向两个人
--                          2. 引导超管（ADMIN_BOOTSTRAP_ACCOUNT）改名后，启动时仍能认出它，不会再建一个
--   password_changed_at  最近一次修改密码的时间，账户安全页展示用
--
-- 可重复执行。

ALTER TABLE admin_accounts
    ADD COLUMN IF NOT EXISTS account_changed_at  TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS previous_account    VARCHAR(64) NULL,
    ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ NULL;

-- 账号按不区分大小写判重。存量里若已有仅大小写不同的两个账号，建索引会失败：
-- 那种情况只记一条提示、不阻断本文件其余部分，服务层的判重同样按 lower() 比较，新数据不受影响。
DO $$
BEGIN
    CREATE UNIQUE INDEX IF NOT EXISTS uq_admin_accounts_account_lower ON admin_accounts (lower(account));
EXCEPTION WHEN unique_violation THEN
    RAISE NOTICE 'admin_accounts 存在仅大小写不同的账号，跳过 uq_admin_accounts_account_lower';
END $$;

CREATE INDEX IF NOT EXISTS idx_admin_accounts_previous_lower
    ON admin_accounts (lower(previous_account))
    WHERE previous_account IS NOT NULL;
