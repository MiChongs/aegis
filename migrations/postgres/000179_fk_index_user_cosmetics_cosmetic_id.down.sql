-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_cosmetics_cosmetic_id_fk;
