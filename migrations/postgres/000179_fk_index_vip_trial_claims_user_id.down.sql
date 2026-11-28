-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_vip_trial_claims_user_id_fk;
