-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_card_keys_bound_user_id_fk;
