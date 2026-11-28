-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_ai_gateway_keys_user_id_fk;
