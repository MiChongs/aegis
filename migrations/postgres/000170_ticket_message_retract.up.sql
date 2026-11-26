-- +migrate Up
-- 工单单条回复撤回（软删除）：行留在库里留档，提单人侧所有接口不再下发，管理端显示为「已撤回」。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。
--
--   deleted_at           撤回时间；NULL 表示正常消息
--   deleted_by_admin_id  撤回人（管理员）；管理员被删后留名
--   deleted_by_name      撤回人显示名
--   delete_reason        撤回原因（可空）

ALTER TABLE ticket_messages ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL;
ALTER TABLE ticket_messages ADD COLUMN IF NOT EXISTS deleted_by_admin_id BIGINT NULL REFERENCES admin_accounts(id) ON DELETE SET NULL;
ALTER TABLE ticket_messages ADD COLUMN IF NOT EXISTS deleted_by_name VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE ticket_messages ADD COLUMN IF NOT EXISTS delete_reason VARCHAR(500) NOT NULL DEFAULT '';

-- 提单人侧只读「对外且未撤回」的消息
CREATE INDEX IF NOT EXISTS idx_ticket_messages_visible
    ON ticket_messages(ticket_id, id ASC) WHERE internal = FALSE AND deleted_at IS NULL;
