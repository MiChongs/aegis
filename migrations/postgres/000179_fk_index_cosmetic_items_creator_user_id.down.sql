-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_cosmetic_items_creator_user_id_fk;
