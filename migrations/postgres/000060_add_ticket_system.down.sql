-- +migrate Down
-- 逆序删除：先删引用方，再删被引用方

DROP TABLE IF EXISTS notify_deliveries;
DROP TABLE IF EXISTS notify_subscriptions;
DROP TABLE IF EXISTS notify_templates;
DROP TABLE IF EXISTS notify_channels;

DROP TABLE IF EXISTS ticket_quick_replies;
DROP TABLE IF EXISTS ticket_watchers;
DROP TABLE IF EXISTS ticket_events;
DROP TABLE IF EXISTS ticket_attachments;
DROP TABLE IF EXISTS ticket_messages;
DROP TABLE IF EXISTS tickets;
DROP TABLE IF EXISTS ticket_categories;
DROP TABLE IF EXISTS ticket_group_members;
DROP TABLE IF EXISTS ticket_groups;
DROP TABLE IF EXISTS ticket_sla_policies;

DROP SEQUENCE IF EXISTS ticket_no_seq;
