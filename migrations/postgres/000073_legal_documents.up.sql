-- +migrate Up
-- 法律文本（用户协议 / 隐私政策）落库。
--
-- 此前这两份文本是**写死在前端组件里的两段 JSX**（terms-of-service.tsx /
-- privacy-policy.tsx），中英文各抄一遍并排渲染。三个后果：
--   1. 改一个字要发一次前端版本，而法律文本的修订往往是有时限要求的；
--   2. 加一种语言等于再抄一整份 JSX，实际上永远只会有中英两版；
--   3. 每个部署方看到的都是这套系统作者写的条款，改不了。
--
-- 表按「文档 × 语言」一行。刻意**不建空表就上线**：服务端内置了
-- 简体中文与英文的默认全文，没有对应行时直接发内置版本，
-- 于是全新部署开箱就有可用的条款，而不是两个空白页面。
CREATE TABLE IF NOT EXISTS legal_documents (
    id           BIGSERIAL PRIMARY KEY,
    -- terms / privacy。不做成枚举：新增一类文本（如 Cookie 声明）时
    -- 枚举要走一次 DDL，而这一列的取值范围本来就由服务端白名单管着。
    doc_type     VARCHAR(32)  NOT NULL,
    -- BCP 47 语言标签（zh-Hans / en / ja …）。大小写与分隔符由服务端归一化后写入，
    -- 否则 "zh-hans" 与 "zh-Hans" 会变成两行，而协商时只认得出其中一行。
    locale       VARCHAR(35)  NOT NULL,
    title        TEXT         NOT NULL,
    -- 纯文本摘要，由服务端从正文提取后落库。列表页与 SEO 描述都只要这一段，
    -- 让每一端各自去解析富文本既慢又会解析出不同结果（与 notices.summary 同一取向）。
    summary      TEXT         NOT NULL DEFAULT '',
    -- 富文本正文，**写入时已净化**。读取端不再净化：净化放在读取端意味着
    -- 每个消费方都要记得做一次，漏一个就是一次存储型 XSS。
    body         TEXT         NOT NULL,
    -- 对外公示的版本号（如 "2026.03"）。留空表示这份文本还没有正式版本号。
    version      VARCHAR(64)  NOT NULL DEFAULT '',
    -- 生效日期。法律文本的「最后更新」不等于「写入时间」——
    -- 条款可以今天写好、下月生效，用 updated_at 冒充会让公示日期是错的。
    effective_at TIMESTAMPTZ  NULL,
    -- 未发布的行对公开接口不可见，但管理员仍能编辑与预览。
    published    BOOLEAN      NOT NULL DEFAULT TRUE,
    -- 不加外键：管理员账号被删除时不应该把条款一起带走（与 notices.created_by 同一取向）。
    updated_by   BIGINT       NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    -- 一个文档在一种语言下只能有一份现行文本。历史版本不在这张表里，
    -- 需要留痕时由审计日志承担 —— 把版本历史塞进同一张表会让
    -- 「取当前生效的那一份」这个最高频的查询变成一次排序扫描。
    CONSTRAINT legal_documents_type_locale_unique UNIQUE (doc_type, locale)
);

-- 公开接口的唯一查询形状：按文档类型取全部已发布语言，然后在内存里协商。
-- 语言数是个位数，先取全量再协商比按语言逐个试快，也少一次「协商结果没有对应行」的往返。
CREATE INDEX IF NOT EXISTS idx_legal_documents_type_published
    ON legal_documents (doc_type, published);

COMMENT ON TABLE legal_documents IS '法律文本（用户协议 / 隐私政策），按文档 × 语言一行；无行时回落到服务端内置默认全文';
