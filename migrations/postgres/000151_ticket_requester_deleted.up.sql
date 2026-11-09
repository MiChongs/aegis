-- 删除用户（管理员删除、账号注销）失败：new row for relation "tickets" violates check constraint "ck_tickets_requester"。
--
-- 原因：tickets.requester_user_id / requester_admin_id 的外键是 ON DELETE SET NULL，删人时数据库把提单人置空，
-- 而 ck_tickets_requester 又要求 user 型工单必须有 requester_user_id（admin 型同理），于是只要这个人提过工单，
-- 删除就整体回滚。管理员账号删除也会撞上同一条约束。
--
-- 设计本意是「人删了，工单与会话留档」（ticket_messages.author_user_id 同样是 SET NULL），
-- 提单人姓名在 requester_name 里有快照。所以：
--   1. CHECK 只保留结构约束：类型与另一侧 ID 互斥，不再要求 ID 非空，允许提单人被删除后置空；
--   2. 「新建工单必须能追溯到提单人」改由触发器在 INSERT 与修改 requester_type 时检查，
--      外键置空动作只更新 requester_user_id / requester_admin_id，不触发这条检查。

ALTER TABLE tickets DROP CONSTRAINT IF EXISTS ck_tickets_requester;
ALTER TABLE tickets ADD CONSTRAINT ck_tickets_requester CHECK (
    (requester_type = 'user'  AND requester_admin_id IS NULL) OR
    (requester_type = 'admin' AND requester_user_id IS NULL)
);

CREATE OR REPLACE FUNCTION tickets_requester_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.requester_type = 'user' AND NEW.requester_user_id IS NULL THEN
        RAISE EXCEPTION 'tickets: user 型工单必须指定 requester_user_id'
            USING ERRCODE = '23514', CONSTRAINT = 'ck_tickets_requester';
    END IF;
    IF NEW.requester_type = 'admin' AND NEW.requester_admin_id IS NULL THEN
        RAISE EXCEPTION 'tickets: admin 型工单必须指定 requester_admin_id'
            USING ERRCODE = '23514', CONSTRAINT = 'ck_tickets_requester';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_tickets_requester_guard ON tickets;
CREATE TRIGGER trg_tickets_requester_guard
    BEFORE INSERT OR UPDATE OF requester_type ON tickets
    FOR EACH ROW EXECUTE FUNCTION tickets_requester_guard();
