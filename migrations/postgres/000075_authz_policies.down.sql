-- +migrate Down

DROP INDEX IF EXISTS idx_authz_policies_subject;
DROP INDEX IF EXISTS idx_authz_policies_group;
DROP INDEX IF EXISTS idx_authz_policies_unique;

DROP TABLE IF EXISTS authz_policies;
