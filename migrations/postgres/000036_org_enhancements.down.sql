ALTER TABLE department_members
    DROP COLUMN IF EXISTS position_id,
    DROP COLUMN IF EXISTS job_title,
    DROP COLUMN IF EXISTS reporting_to,
    DROP COLUMN IF EXISTS delegate_to,
    DROP COLUMN IF EXISTS delegate_expires_at;

DROP TABLE IF EXISTS positions;
