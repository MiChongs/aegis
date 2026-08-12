-- +migrate Up
-- 会员功能标识（feature tag）与服务端校验。
--
-- 在此之前「会员」只有一个维度：是或不是。于是接入方一旦有两档会员
-- （基础版能导出、高级版还能用 AI），后端就无从判断"这个人能不能用这个功能"——
-- 只能在客户端按套餐名做字符串比较，而套餐名是随时会被运营改掉的展示文案。
--
-- 现在多了一层**功能标识**：
--
--   vip_features   应用维护的功能目录（tag → 展示名）
--   vip_plans.features        这个套餐包含哪些功能
--   vip_transactions.features 开通那一刻的功能快照
--
-- 快照是必须的：套餐配置随时会改，而**已经卖出去的权益不该被追溯改写**。
-- 用户买的是"当时那份包含 export 的高级版"，运营明天把 export 从套餐里拿掉，
-- 不能让他手上的会员当场少一个功能。

-- ── 功能目录 ──
--
-- 做成目录而不是让接入方随手传字符串：拼错一个字母（exprot）的表现是
-- 校验永远返回 false，没有任何报错说得出为什么。有目录才能在校验入口
-- 明确回一句「这个功能标识没登记过」。
CREATE TABLE IF NOT EXISTS vip_features (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    -- tag 是机器标识，接入方在校验时传的就是它；name 是给运营看的
    tag VARCHAR(64) NOT NULL,
    name VARCHAR(64) NOT NULL,
    description TEXT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_vip_features_tag UNIQUE (appid, tag)
);

CREATE INDEX IF NOT EXISTS idx_vip_features_app ON vip_features(appid, sort_order, id);

-- ── 套餐包含的功能 ──
ALTER TABLE vip_plans
    ADD COLUMN IF NOT EXISTS features TEXT[] NOT NULL DEFAULT '{}';

-- ── 开通时的功能快照 ──
ALTER TABLE vip_transactions
    ADD COLUMN IF NOT EXISTS features TEXT[] NOT NULL DEFAULT '{}';

-- 校验热路径的形状：某用户在本应用**尚未到期**的开通记录。
-- 会员期是顺延的，因此同一时刻可能有多笔仍未到期的记录（先买 A 再买 B），
-- 当前功能权益是它们的并集 —— 这条索引就是为那次聚合准备的。
CREATE INDEX IF NOT EXISTS idx_vip_transactions_active
    ON vip_transactions(appid, user_id, expire_after DESC);
