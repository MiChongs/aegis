-- +migrate Down
DROP INDEX IF EXISTS idx_ticket_messages_visible;
ALTER TABLE ticket_messages DROP COLUMN IF EXISTS delete_reason;
ALTER TABLE ticket_messages DROP COLUMN IF EXISTS deleted_by_name;
ALTER TABLE ticket_messages DROP COLUMN IF EXISTS deleted_by_admin_id;
ALTER TABLE ticket_messages DROP COLUMN IF EXISTS deleted_at;
