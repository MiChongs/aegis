-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_ai_gateway_requests_user_id_fk;
