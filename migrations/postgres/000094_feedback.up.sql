-- +migrate Up
-- 意见反馈：反馈是 kind = 'feedback' 的工单，复用工单的表、处理流程与通知链路，
-- 只多一个「类型」维度。分类、工单、附件各加一列 kind：
--   ticket_categories.kind  ticket | feedback   —— 反馈入口只列 feedback 分类，工单入口只列 ticket 分类
--   tickets.kind            ticket | feedback   —— 用户的「我的工单」与「我的反馈」互不混入
--   ticket_attachments.kind image | file        —— image 由服务端按魔数判定，不信客户端声明

ALTER TABLE ticket_categories ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'ticket';
ALTER TABLE ticket_categories DROP CONSTRAINT IF EXISTS ck_ticket_categories_kind;
ALTER TABLE ticket_categories ADD CONSTRAINT ck_ticket_categories_kind CHECK (kind IN ('ticket','feedback'));

ALTER TABLE tickets ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'ticket';
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS ck_tickets_kind;
ALTER TABLE tickets ADD CONSTRAINT ck_tickets_kind CHECK (kind IN ('ticket','feedback'));

ALTER TABLE ticket_attachments ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'file';
ALTER TABLE ticket_attachments DROP CONSTRAINT IF EXISTS ck_ticket_attachments_kind;
ALTER TABLE ticket_attachments ADD CONSTRAINT ck_ticket_attachments_kind CHECK (kind IN ('image','file'));

-- 存量附件按已落库的类型回填，好让管理端的图片数从第一天就是对的
UPDATE ticket_attachments SET kind = 'image'
 WHERE kind = 'file' AND content_type IN ('image/png','image/jpeg','image/webp','image/gif');

-- 「我的反馈」：应用 + 类型 + 提单人 + 时间倒序；频率限制的计数也走这条
CREATE INDEX IF NOT EXISTS idx_tickets_kind_requester ON tickets(appid, kind, requester_user_id, created_at DESC);
-- 管理端按类型筛选
CREATE INDEX IF NOT EXISTS idx_tickets_kind_status ON tickets(kind, status, created_at DESC);

-- 官网提交的反馈来源记为 web
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS tickets_source_check;
ALTER TABLE tickets ADD CONSTRAINT tickets_source_check
    CHECK (source IN ('console','app','api','email','bot','import','web'));

-- 平台级反馈分类，所有应用共用；应用可在控制台另建自己的 feedback 分类
INSERT INTO ticket_categories (appid, key, name, description, kind, sort, user_submittable, enabled)
VALUES
    (0, 'feedback_bug', '问题反馈', '功能异常、报错或闪退', 'feedback', 10, TRUE, TRUE),
    (0, 'feedback_feature', '功能建议', '希望新增或改进的功能', 'feedback', 20, TRUE, TRUE),
    (0, 'feedback_experience', '体验吐槽', '界面、交互或性能上的不满', 'feedback', 30, TRUE, TRUE),
    (0, 'feedback_other', '其他', '其他意见与建议', 'feedback', 40, TRUE, TRUE)
ON CONFLICT (appid, key) DO NOTHING;
