DROP TRIGGER IF EXISTS trg_tickets_requester_guard ON tickets;
DROP FUNCTION IF EXISTS tickets_requester_guard();

-- 回到旧约束前，提单人已被删除的工单无法满足它：改挂到 NOT VALID，只约束此后的新行。
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS ck_tickets_requester;
ALTER TABLE tickets ADD CONSTRAINT ck_tickets_requester CHECK (
    (requester_type = 'user'  AND requester_user_id IS NOT NULL) OR
    (requester_type = 'admin' AND requester_admin_id IS NOT NULL)
) NOT VALID;
