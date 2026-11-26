-- +migrate Up
-- 工单 / 意见反馈状态精简：状态一律三个字，共七种（internal/domain/ticket/types.go）。
--   待受理 open、处理中 processing、等待中 pending、已解决 resolved、不采纳 rejected、已关闭 closed、已撤销 cancelled
-- 「待用户补充」「等待第三方」合并为「等待中」；新增「不采纳」，与已关闭、已撤销同为终态。

ALTER TABLE tickets DROP CONSTRAINT IF EXISTS tickets_status_check;

UPDATE tickets SET status = 'pending' WHERE status IN ('pending_user', 'pending_third_party');
UPDATE ticket_events SET from_value = 'pending' WHERE from_value IN ('pending_user', 'pending_third_party');
UPDATE ticket_events SET to_value = 'pending' WHERE to_value IN ('pending_user', 'pending_third_party');

ALTER TABLE tickets ADD CONSTRAINT tickets_status_check
    CHECK (status IN ('open','processing','pending','resolved','rejected','closed','cancelled'));

-- 「未终结」的部分索引把不采纳一并排除
DROP INDEX IF EXISTS idx_tickets_assignee_open;
CREATE INDEX IF NOT EXISTS idx_tickets_assignee_open ON tickets(assignee_admin_id, updated_at DESC)
    WHERE status NOT IN ('closed','cancelled','rejected');
DROP INDEX IF EXISTS idx_tickets_group_open;
CREATE INDEX IF NOT EXISTS idx_tickets_group_open ON tickets(group_id, updated_at DESC)
    WHERE status NOT IN ('closed','cancelled','rejected');
DROP INDEX IF EXISTS idx_tickets_sla_scan;
CREATE INDEX IF NOT EXISTS idx_tickets_sla_scan ON tickets(first_response_due_at, resolve_due_at)
    WHERE status NOT IN ('closed','cancelled','rejected','resolved');
