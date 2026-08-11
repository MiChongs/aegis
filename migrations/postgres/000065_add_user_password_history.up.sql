-- 密码历史：支撑密码策略里的 preventReuse（禁止重用近期 N 个密码）。
--
-- 此前 preventReuse 只落在应用 settings 里、没有任何执行点，
-- 管理员配了「禁止重用最近 5 个密码」实际毫无约束。
--
-- 只存哈希，不存任何可还原为明文的内容；bcrypt 每条哈希自带 salt，
-- 因此判重必须逐条 CompareHashAndPassword，无法靠等值查询完成 ——
-- 这也是为什么这里要限制保留条数（策略上限 20 条），
-- 否则每次改密都要跑几十次 bcrypt。
CREATE TABLE IF NOT EXISTS user_password_history (
  id            BIGSERIAL PRIMARY KEY,
  user_id       BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  password_hash TEXT        NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 取「某用户最近 N 条」是唯一的读模式
CREATE INDEX IF NOT EXISTS idx_user_password_history_user_created
  ON user_password_history (user_id, created_at DESC);
