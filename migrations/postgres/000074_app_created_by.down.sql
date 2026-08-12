-- +migrate Down

DROP INDEX IF EXISTS idx_apps_created_by;

ALTER TABLE apps DROP COLUMN IF EXISTS created_by;
