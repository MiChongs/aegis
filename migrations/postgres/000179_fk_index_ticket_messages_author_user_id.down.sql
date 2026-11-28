-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_ticket_messages_author_user_id_fk;
