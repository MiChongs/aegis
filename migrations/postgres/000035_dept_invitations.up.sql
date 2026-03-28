CREATE TABLE IF NOT EXISTS department_invitations (
    id BIGSERIAL PRIMARY KEY,
    department_id BIGINT NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    inviter_id BIGINT NOT NULL REFERENCES admin_accounts(id),
    invitee_id BIGINT NOT NULL REFERENCES admin_accounts(id),
    is_leader BOOLEAN DEFAULT FALSE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    message TEXT DEFAULT '',
    responded_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '7 days',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_dept_inv_pending_unique
    ON department_invitations(department_id, invitee_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_dept_inv_invitee ON department_invitations(invitee_id, status);
CREATE INDEX IF NOT EXISTS idx_dept_inv_dept ON department_invitations(department_id, status);
