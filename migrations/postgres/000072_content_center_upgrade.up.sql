-- +migrate Up
-- 内容中心升级：把应用级公告从「标题 + 正文」补成有生命周期的投放对象，
-- 并给 Banner 的 type 一个真正的语义。
--
-- 公告此前只有 title / content 两列，于是三件运营上必然要做的事都做不了：
--   1. 写好了先不发（没有草稿态，存进去就对所有客户端可见）
--   2. 到点自动上下线（没有时间窗，只能人肉守着删除）
--   3. 重要的排在前面（没有置顶，只能按创建时间倒序）
-- 补的这几列全部带默认值，存量行落在「已发布 / 无时间窗 / 普通 / 不置顶」，
-- 与升级前的行为完全一致。

ALTER TABLE notices
    ADD COLUMN IF NOT EXISTS type VARCHAR(32) NOT NULL DEFAULT 'notice',
    ADD COLUMN IF NOT EXISTS level VARCHAR(16) NOT NULL DEFAULT 'normal',
    ADD COLUMN IF NOT EXISTS status VARCHAR(16) NOT NULL DEFAULT 'published',
    ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE,
    -- summary 是正文的纯文本摘要，由服务端从 HTML 提取后落库。
    -- 列表页与推送都只要这一段，让它们各自去解析一遍富文本既慢又会解析出不同结果。
    ADD COLUMN IF NOT EXISTS summary TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS start_time TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS end_time TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS view_count BIGINT NOT NULL DEFAULT 0,
    -- created_by 不加外键：管理员账号被删除时不应该把公告一起带走，
    -- 与 platform_banners.created_by 同一取向。
    ADD COLUMN IF NOT EXISTS created_by BIGINT NULL;

-- 存量行视作「创建即发布」，否则升级后它们会因为 published_at 为空而失去排序依据。
UPDATE notices SET published_at = created_at WHERE published_at IS NULL AND status = 'published';

-- 展示端的主查询：某应用下已发布的公告，置顶优先、再按发布时间倒序。
CREATE INDEX IF NOT EXISTS idx_notices_app_published
    ON notices(appid, pinned DESC, published_at DESC NULLS LAST, id DESC)
    WHERE status = 'published';

-- 时间窗过滤与管理端按状态筛选。
CREATE INDEX IF NOT EXISTS idx_notices_window ON notices(appid, start_time, end_time);
CREATE INDEX IF NOT EXISTS idx_notices_app_status ON notices(appid, status);

-- Banner 的 type 原本没有任何读取点，默认值 'url' 也说不出是什么意思。
-- 现在它表示**展示位**（客户端据此决定这条 Banner 画在哪儿），
-- 取值收敛到 hero / popup / splash / notice / card 五档，存量值统一落到首页轮播。
UPDATE banners SET type = 'hero'
    WHERE type IS NULL OR btrim(type) = '' OR type NOT IN ('hero', 'popup', 'splash', 'notice', 'card');

ALTER TABLE banners ALTER COLUMN type SET DEFAULT 'hero';

-- 展示端只看启用中的那些，条件索引比全表索引小一个数量级。
CREATE INDEX IF NOT EXISTS idx_banners_app_active
    ON banners(appid, type, position ASC)
    WHERE status = TRUE;
