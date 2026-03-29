package postgres

import (
	"context"
	"time"

	admindomain "aegis/internal/domain/admin"
	"aegis/pkg/timeutil"

	"github.com/jackc/pgx/v5"
)

// ── 会话管理 ──

// CreateAdminSessionRecord 写入管理员会话持久化记录（id 即 JWT JTI）
func (r *Repository) CreateAdminSessionRecord(ctx context.Context, record admindomain.AdminSessionRecord) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO admin_sessions (id, admin_id, ip, user_agent, device, issued_at, expires_at, last_active_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $6)
ON CONFLICT (id) DO NOTHING`,
		record.ID, record.AdminID, record.IP, record.UserAgent, record.Device, record.IssuedAt, record.ExpiresAt)
	return err
}

// ListAdminSessions 列出指定管理员的会话记录
func (r *Repository) ListAdminSessions(ctx context.Context, adminID int64) ([]admindomain.AdminSessionRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, admin_id, ip, user_agent, device, issued_at, expires_at, last_active_at, is_revoked, revoked_by, revoked_at
FROM admin_sessions
WHERE admin_id = $1 AND NOT is_revoked AND expires_at > NOW()
ORDER BY issued_at DESC`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAdminSessionRows(rows)
}

// GetAdminSessionByID 获取指定管理员会话记录
func (r *Repository) GetAdminSessionByID(ctx context.Context, sessionID string) (*admindomain.AdminSessionRecord, error) {
	var item admindomain.AdminSessionRecord
	err := r.pool.QueryRow(ctx,
		`SELECT id, admin_id, ip, user_agent, device, issued_at, expires_at, last_active_at, is_revoked, revoked_by, revoked_at
FROM admin_sessions
WHERE id = $1`, sessionID,
	).Scan(
		&item.ID, &item.AdminID, &item.IP, &item.UserAgent, &item.Device,
		&item.IssuedAt, &item.ExpiresAt, &item.LastActiveAt,
		&item.IsRevoked, &item.RevokedBy, &item.RevokedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

// ListAllActiveSessions 分页列出所有活跃会话（LEFT JOIN 填充管理员信息）
func (r *Repository) ListAllActiveSessions(ctx context.Context, page, limit int) ([]admindomain.AdminSessionRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_sessions WHERE NOT is_revoked AND expires_at > NOW()`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx,
		`SELECT s.id, s.admin_id, s.ip, s.user_agent, s.device, s.issued_at, s.expires_at, s.last_active_at,
		        s.is_revoked, s.revoked_by, s.revoked_at,
		        COALESCE(a.account, ''), COALESCE(a.display_name, '')
FROM admin_sessions s
LEFT JOIN admin_accounts a ON a.id = s.admin_id
WHERE NOT s.is_revoked AND s.expires_at > NOW()
ORDER BY s.last_active_at DESC
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]admindomain.AdminSessionRecord, 0, limit)
	for rows.Next() {
		var item admindomain.AdminSessionRecord
		if err := rows.Scan(
			&item.ID, &item.AdminID, &item.IP, &item.UserAgent, &item.Device,
			&item.IssuedAt, &item.ExpiresAt, &item.LastActiveAt,
			&item.IsRevoked, &item.RevokedBy, &item.RevokedAt,
			&item.AdminAccount, &item.AdminName,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// RevokeAdminSession 撤销单个会话
func (r *Repository) RevokeAdminSession(ctx context.Context, sessionID string, revokedBy int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE admin_sessions SET is_revoked = TRUE, revoked_by = $2, revoked_at = NOW() WHERE id = $1 AND NOT is_revoked`,
		sessionID, revokedBy)
	return err
}

// RevokeAllAdminSessions 撤销指定管理员的所有活跃会话，返回影响行数
func (r *Repository) RevokeAllAdminSessions(ctx context.Context, adminID, revokedBy int64) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE admin_sessions SET is_revoked = TRUE, revoked_by = $2, revoked_at = NOW() WHERE admin_id = $1 AND NOT is_revoked`,
		adminID, revokedBy)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// IsSessionRevoked 检查会话是否已被撤销
func (r *Repository) IsSessionRevoked(ctx context.Context, sessionID string) (bool, error) {
	var revoked bool
	err := r.pool.QueryRow(ctx, `SELECT is_revoked FROM admin_sessions WHERE id = $1`, sessionID).Scan(&revoked)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil // 没有记录视为未撤销（兼容旧会话）
		}
		return false, err
	}
	return revoked, nil
}

// UpdateSessionLastActive 更新会话最后活跃时间
func (r *Repository) UpdateSessionLastActive(ctx context.Context, sessionID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE admin_sessions SET last_active_at = NOW() WHERE id = $1`, sessionID)
	return err
}

// CleanupExpiredSessions 清理已过期的会话记录
func (r *Repository) CleanupExpiredSessions(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at < $1`, timeutil.NormalizeUTC(cutoff))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 临时权限 ──

// GrantTempPermission 授予临时权限
func (r *Repository) GrantTempPermission(ctx context.Context, adminID int64, permission string, appID *int64, grantedBy int64, reason string, expiresAt time.Time) (*admindomain.TempPermission, error) {
	var item admindomain.TempPermission
	err := r.pool.QueryRow(ctx,
		`INSERT INTO admin_temp_permissions (admin_id, permission, app_id, granted_by, reason, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, admin_id, permission, app_id, granted_by, reason, expires_at, is_revoked, created_at`,
		adminID, permission, nullableInt64Value(appID), grantedBy, reason, expiresAt,
	).Scan(&item.ID, &item.AdminID, &item.Permission, &item.AppID, &item.GrantedBy, &item.Reason, &item.ExpiresAt, &item.IsRevoked, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ListTempPermissions 列出指定管理员的活跃临时权限
func (r *Repository) ListTempPermissions(ctx context.Context, adminID int64) ([]admindomain.TempPermission, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT tp.id, tp.admin_id, tp.permission, tp.app_id, tp.granted_by, tp.reason, tp.expires_at, tp.is_revoked, tp.created_at,
		        COALESCE(a.account, ''), COALESCE(g.display_name, '')
FROM admin_temp_permissions tp
LEFT JOIN admin_accounts a ON a.id = tp.admin_id
LEFT JOIN admin_accounts g ON g.id = tp.granted_by
WHERE tp.admin_id = $1 AND NOT tp.is_revoked AND tp.expires_at > NOW()
ORDER BY tp.created_at DESC`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTempPermissionRows(rows)
}

// ListAllTempPermissions 列出所有活跃临时权限（超管查看）
func (r *Repository) ListAllTempPermissions(ctx context.Context) ([]admindomain.TempPermission, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT tp.id, tp.admin_id, tp.permission, tp.app_id, tp.granted_by, tp.reason, tp.expires_at, tp.is_revoked, tp.created_at,
		        COALESCE(a.account, ''), COALESCE(g.display_name, '')
FROM admin_temp_permissions tp
LEFT JOIN admin_accounts a ON a.id = tp.admin_id
LEFT JOIN admin_accounts g ON g.id = tp.granted_by
WHERE NOT tp.is_revoked AND tp.expires_at > NOW()
ORDER BY tp.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTempPermissionRows(rows)
}

// RevokeTempPermission 撤销临时权限
func (r *Repository) RevokeTempPermission(ctx context.Context, permID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE admin_temp_permissions SET is_revoked = TRUE WHERE id = $1`, permID)
	return err
}

// GetActiveTempPermissions 获取管理员当前活跃的临时权限代码列表
func (r *Repository) GetActiveTempPermissions(ctx context.Context, adminID int64) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT permission FROM admin_temp_permissions WHERE admin_id = $1 AND NOT is_revoked AND expires_at > NOW()`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			return nil, err
		}
		items = append(items, perm)
	}
	return items, rows.Err()
}

// CleanupExpiredTempPermissions 标记过期的临时权限为已撤销
func (r *Repository) CleanupExpiredTempPermissions(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE admin_temp_permissions SET is_revoked = TRUE WHERE expires_at < $1 AND NOT is_revoked`, timeutil.NormalizeUTC(cutoff))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 代理授权 ──

// CreateDelegation 创建代理授权
func (r *Repository) CreateDelegation(ctx context.Context, delegation admindomain.AdminDelegation) (*admindomain.AdminDelegation, error) {
	var item admindomain.AdminDelegation
	err := r.pool.QueryRow(ctx,
		`INSERT INTO admin_delegations (delegator_id, delegate_id, scope, scope_id, granted_by, reason, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, delegator_id, delegate_id, scope, scope_id, granted_by, reason, expires_at, is_revoked, created_at`,
		delegation.DelegatorID, delegation.DelegateID, delegation.Scope,
		nullableInt64Value(delegation.ScopeID), delegation.GrantedBy, delegation.Reason, delegation.ExpiresAt,
	).Scan(&item.ID, &item.DelegatorID, &item.DelegateID, &item.Scope, &item.ScopeID,
		&item.GrantedBy, &item.Reason, &item.ExpiresAt, &item.IsRevoked, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ListDelegations 列出代理授权（role="delegator"→ 我委派出去的；role="delegate"→ 被委派给我的）
func (r *Repository) ListDelegations(ctx context.Context, adminID int64, role string) ([]admindomain.AdminDelegation, error) {
	var condition string
	if role == "delegate" {
		condition = "d.delegate_id = $1"
	} else {
		condition = "d.delegator_id = $1"
	}
	query := `SELECT d.id, d.delegator_id, d.delegate_id, d.scope, d.scope_id, d.granted_by, d.reason, d.expires_at, d.is_revoked, d.created_at,
	       COALESCE(dr.display_name, ''), COALESCE(de.display_name, '')
FROM admin_delegations d
LEFT JOIN admin_accounts dr ON dr.id = d.delegator_id
LEFT JOIN admin_accounts de ON de.id = d.delegate_id
WHERE ` + condition + ` AND NOT d.is_revoked AND d.expires_at > NOW()
ORDER BY d.created_at DESC`
	rows, err := r.pool.Query(ctx, query, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDelegationRows(rows)
}

// RevokeDelegation 撤销代理授权
func (r *Repository) RevokeDelegation(ctx context.Context, delegationID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE admin_delegations SET is_revoked = TRUE WHERE id = $1`, delegationID)
	return err
}

// GetActiveDelegationsForDelegate 获取被委派给指定管理员的活跃代理列表
func (r *Repository) GetActiveDelegationsForDelegate(ctx context.Context, delegateID int64) ([]admindomain.AdminDelegation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT d.id, d.delegator_id, d.delegate_id, d.scope, d.scope_id, d.granted_by, d.reason, d.expires_at, d.is_revoked, d.created_at,
		        COALESCE(dr.display_name, ''), COALESCE(de.display_name, '')
FROM admin_delegations d
LEFT JOIN admin_accounts dr ON dr.id = d.delegator_id
LEFT JOIN admin_accounts de ON de.id = d.delegate_id
WHERE d.delegate_id = $1 AND NOT d.is_revoked AND d.expires_at > NOW()
ORDER BY d.created_at DESC`, delegateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDelegationRows(rows)
}

// CleanupExpiredDelegations 标记过期的代理授权为已撤销
func (r *Repository) CleanupExpiredDelegations(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE admin_delegations SET is_revoked = TRUE WHERE expires_at < $1 AND NOT is_revoked`, timeutil.NormalizeUTC(cutoff))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 扫描辅助函数 ──

func scanAdminSessionRows(rows pgx.Rows) ([]admindomain.AdminSessionRecord, error) {
	items := make([]admindomain.AdminSessionRecord, 0, 8)
	for rows.Next() {
		var item admindomain.AdminSessionRecord
		if err := rows.Scan(
			&item.ID, &item.AdminID, &item.IP, &item.UserAgent, &item.Device,
			&item.IssuedAt, &item.ExpiresAt, &item.LastActiveAt,
			&item.IsRevoked, &item.RevokedBy, &item.RevokedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanTempPermissionRows(rows pgx.Rows) ([]admindomain.TempPermission, error) {
	items := make([]admindomain.TempPermission, 0, 8)
	for rows.Next() {
		var item admindomain.TempPermission
		if err := rows.Scan(
			&item.ID, &item.AdminID, &item.Permission, &item.AppID, &item.GrantedBy,
			&item.Reason, &item.ExpiresAt, &item.IsRevoked, &item.CreatedAt,
			&item.AdminAccount, &item.GranterName,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanDelegationRows(rows pgx.Rows) ([]admindomain.AdminDelegation, error) {
	items := make([]admindomain.AdminDelegation, 0, 8)
	for rows.Next() {
		var item admindomain.AdminDelegation
		if err := rows.Scan(
			&item.ID, &item.DelegatorID, &item.DelegateID, &item.Scope, &item.ScopeID,
			&item.GrantedBy, &item.Reason, &item.ExpiresAt, &item.IsRevoked, &item.CreatedAt,
			&item.DelegatorName, &item.DelegateName,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
