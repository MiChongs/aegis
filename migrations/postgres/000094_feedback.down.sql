-- +migrate Down
DELETE FROM ticket_categories
 WHERE appid = 0 AND key IN ('feedback_bug','feedback_feature','feedback_experience','feedback_other');

UPDATE tickets SET source = 'app' WHERE source = 'web';
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS tickets_source_check;
ALTER TABLE tickets ADD CONSTRAINT tickets_source_check
    CHECK (source IN ('console','app','api','email','bot','import'));

DROP INDEX IF EXISTS idx_tickets_kind_status;
DROP INDEX IF EXISTS idx_tickets_kind_requester;

ALTER TABLE ticket_attachments DROP CONSTRAINT IF EXISTS ck_ticket_attachments_kind;
ALTER TABLE ticket_attachments DROP COLUMN IF EXISTS kind;
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS ck_tickets_kind;
ALTER TABLE tickets DROP COLUMN IF EXISTS kind;
ALTER TABLE ticket_categories DROP CONSTRAINT IF EXISTS ck_ticket_categories_kind;
ALTER TABLE ticket_categories DROP COLUMN IF EXISTS kind;
