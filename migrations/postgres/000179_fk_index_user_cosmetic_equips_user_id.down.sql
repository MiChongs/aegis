-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_user_cosmetic_equips_user_id_fk;
