-- +migrate Down
-- 等待中无法区分原来等的是谁，一律回到「待用户补充」；不采纳回到已关闭。
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS tickets_status_check;

UPDATE tickets SET status = 'pending_user' WHERE status = 'pending';
UPDATE tickets SET status = 'closed' WHERE status = 'rejected';
UPDATE ticket_events SET from_value = 'pending_user' WHERE from_value = 'pending';
UPDATE ticket_events SET to_value = 'pending_user' WHERE to_value = 'pending';

ALTER TABLE tickets ADD CONSTRAINT tickets_status_check
    CHECK (status IN ('open','processing','pending_user','pending_third_party','resolved','closed','cancelled'));

DROP INDEX IF EXISTS idx_tickets_assignee_open;
CREATE INDEX IF NOT EXISTS idx_tickets_assignee_open ON tickets(assignee_admin_id, updated_at DESC)
    WHERE status NOT IN ('closed','cancelled');
DROP INDEX IF EXISTS idx_tickets_group_open;
CREATE INDEX IF NOT EXISTS idx_tickets_group_open ON tickets(group_id, updated_at DESC)
    WHERE status NOT IN ('closed','cancelled');
DROP INDEX IF EXISTS idx_tickets_sla_scan;
CREATE INDEX IF NOT EXISTS idx_tickets_sla_scan ON tickets(first_response_due_at, resolve_due_at)
    WHERE status NOT IN ('closed','cancelled','resolved');
