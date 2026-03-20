package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	userdomain "aegis/internal/domain/user"
)

func (r *Repository) EnsureDefaultRoleDefinitions(ctx context.Context, appID int64) error {
	defaults := []struct {
		key, name, description string
		priority               int
	}{
		{"user", "普通用户", "默认基础角色", 10},
		{"tester", "测试员", "测试与反馈权限", 20},
		{"auditor", "审核员", "审核相关权限", 30},
		{"sharer", "分享者", "内容分享相关权限", 40},
		{"admin", "管理员", "高级管理权限", 100},
	}
	for _, item := range defaults {
		if _, err := r.pool.Exec(ctx, `INSERT INTO role_definitions (appid, role_key, role_name, description, priority, is_enabled, metadata, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, true, '{}'::jsonb, NOW(), NOW())
ON CONFLICT (appid, role_key) DO UPDATE SET role_name = EXCLUDED.role_name, description = EXCLUDED.description, priority = EXCLUDED.priority, updated_at = NOW()`,
			appID, item.key, item.name, item.description, item.priority); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListAvailableRoles(ctx context.Context, appID int64) ([]userdomain.RoleDefinition, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, appid, role_key, role_name, description, priority, is_enabled, COALESCE(metadata, '{}'::jsonb), created_at, updated_at FROM role_definitions WHERE appid = $1 AND is_enabled = true ORDER BY priority DESC, id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userdomain.RoleDefinition, 0, 8)
	for rows.Next() {
		item, err := scanRoleDefinition(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateRoleApplication(ctx context.Context, item userdomain.RoleApplication) (*userdomain.RoleApplication, error) {
	deviceJSON, _ := json.Marshal(item.DeviceInfo)
	extraJSON, _ := json.Marshal(item.Extra)
	query := `INSERT INTO role_applications (appid, user_id, requested_role, current_role_key, reason, status, priority, valid_days, device_info, extra, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 'pending', $6, $7, $8, $9, NOW(), NOW())
RETURNING id, appid, user_id, requested_role, current_role_key, reason, status, priority, valid_days, review_reason, reviewed_by, reviewed_by_name, reviewed_at, cancelled_at, device_info, extra, created_at, updated_at`
	return scanRoleApplication(r.pool.QueryRow(ctx, query, item.AppID, item.UserID, item.RequestedRole, item.CurrentRole, item.Reason, item.Priority, item.ValidDays, deviceJSON, extraJSON))
}

func (r *Repository) HasPendingRoleApplication(ctx context.Context, appID int64, userID int64, requestedRole string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM role_applications WHERE appid = $1 AND user_id = $2 AND requested_role = $3 AND status = 'pending')`, appID, userID, requestedRole).Scan(&exists)
	return exists, err
}

func (r *Repository) GetRoleApplicationByID(ctx context.Context, id int64, appID int64) (*userdomain.RoleApplication, error) {
	query := `SELECT ra.id, ra.appid, ra.user_id, ra.requested_role, ra.current_role_key, ra.reason, ra.status, ra.priority, ra.valid_days, COALESCE(ra.review_reason, ''), ra.reviewed_by, COALESCE(ra.reviewed_by_name, ''), ra.reviewed_at, ra.cancelled_at, COALESCE(ra.device_info, '{}'::jsonb), COALESCE(ra.extra, '{}'::jsonb), ra.created_at, ra.updated_at,
COALESCE(u.account, ''), COALESCE(p.nickname, ''), COALESCE(p.avatar, '')
FROM role_applications ra
JOIN users u ON u.id = ra.user_id
LEFT JOIN user_profiles p ON p.user_id = ra.user_id
WHERE ra.id = $1 AND ra.appid = $2 LIMIT 1`
	return scanRoleApplicationWithUser(r.pool.QueryRow(ctx, query, id, appID))
}

func (r *Repository) ListRoleApplicationsByUser(ctx context.Context, userID int64, appID int64, query userdomain.RoleApplicationListQuery) (*userdomain.RoleApplicationListResult, error) {
	return r.listRoleApplications(ctx, appID, query, fmt.Sprintf("ra.user_id = %d", userID))
}

func (r *Repository) ListRoleApplicationsByApp(ctx context.Context, appID int64, query userdomain.RoleApplicationListQuery) (*userdomain.RoleApplicationListResult, error) {
	return r.listRoleApplications(ctx, appID, query, "TRUE")
}

func (r *Repository) listRoleApplications(ctx context.Context, appID int64, query userdomain.RoleApplicationListQuery, scope string) (*userdomain.RoleApplicationListResult, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	args := []any{appID}
	base := ` FROM role_applications ra
JOIN users u ON u.id = ra.user_id
LEFT JOIN user_profiles p ON p.user_id = ra.user_id
WHERE ra.appid = $1 AND ` + scope
	if query.Status != "" && query.Status != "all" {
		args = append(args, query.Status)
		base += fmt.Sprintf(" AND ra.status = $%d", len(args))
	}
	if query.RequestedRole != "" {
		args = append(args, query.RequestedRole)
		base += fmt.Sprintf(" AND ra.requested_role = $%d", len(args))
	}
	if query.Priority != "" {
		args = append(args, query.Priority)
		base += fmt.Sprintf(" AND ra.priority = $%d", len(args))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		base += fmt.Sprintf(" AND (COALESCE(u.account,'') ILIKE $%d OR COALESCE(p.nickname,'') ILIKE $%d OR ra.reason ILIKE $%d OR ra.requested_role ILIKE $%d)", len(args), len(args), len(args), len(args))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+base, args...).Scan(&total); err != nil {
		return nil, err
	}
	orderBy := "ra.created_at DESC, ra.id DESC"
	if query.SortBy == "priority" {
		orderBy = "ra.priority DESC, ra.created_at DESC"
	}
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, `SELECT ra.id, ra.appid, ra.user_id, ra.requested_role, ra.current_role_key, ra.reason, ra.status, ra.priority, ra.valid_days, COALESCE(ra.review_reason, ''), ra.reviewed_by, COALESCE(ra.reviewed_by_name, ''), ra.reviewed_at, ra.cancelled_at, COALESCE(ra.device_info, '{}'::jsonb), COALESCE(ra.extra, '{}'::jsonb), ra.created_at, ra.updated_at,
COALESCE(u.account, ''), COALESCE(p.nickname, ''), COALESCE(p.avatar, '')`+base+fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", orderBy, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userdomain.RoleApplication, 0, limit)
	for rows.Next() {
		item, err := scanRoleApplicationWithUser(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}
	return &userdomain.RoleApplicationListResult{Items: items, Page: page, Limit: limit, Total: total, TotalPages: totalPages}, rows.Err()
}

func (r *Repository) CancelRoleApplication(ctx context.Context, id int64, userID int64, appID int64) (*userdomain.RoleApplication, error) {
	query := `UPDATE role_applications SET status = 'cancelled', cancelled_at = NOW(), updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND appid = $3 AND status = 'pending'
RETURNING id, appid, user_id, requested_role, current_role_key, reason, status, priority, valid_days, COALESCE(review_reason, ''), reviewed_by, COALESCE(reviewed_by_name, ''), reviewed_at, cancelled_at, COALESCE(device_info, '{}'::jsonb), COALESCE(extra, '{}'::jsonb), created_at, updated_at`
	return scanRoleApplication(r.pool.QueryRow(ctx, query, id, userID, appID))
}

func (r *Repository) ResubmitRoleApplication(ctx context.Context, id int64, userID int64, appID int64, reason string) (*userdomain.RoleApplication, error) {
	query := `UPDATE role_applications SET reason = COALESCE(NULLIF($1, ''), reason), status = 'pending', review_reason = '', reviewed_by = NULL, reviewed_by_name = '', reviewed_at = NULL, cancelled_at = NULL, updated_at = NOW()
WHERE id = $2 AND user_id = $3 AND appid = $4 AND status IN ('rejected', 'cancelled')
RETURNING id, appid, user_id, requested_role, current_role_key, reason, status, priority, valid_days, COALESCE(review_reason, ''), reviewed_by, COALESCE(reviewed_by_name, ''), reviewed_at, cancelled_at, COALESCE(device_info, '{}'::jsonb), COALESCE(extra, '{}'::jsonb), created_at, updated_at`
	return scanRoleApplication(r.pool.QueryRow(ctx, query, reason, id, userID, appID))
}

func (r *Repository) ReviewRoleApplication(ctx context.Context, id int64, appID int64, adminID int64, adminName string, status string, reason string) (*userdomain.RoleApplication, error) {
	query := `UPDATE role_applications SET status = $1, review_reason = $2, reviewed_by = $3, reviewed_by_name = $4, reviewed_at = NOW(), updated_at = NOW()
WHERE id = $5 AND appid = $6 AND status = 'pending'
RETURNING id, appid, user_id, requested_role, current_role_key, reason, status, priority, valid_days, COALESCE(review_reason, ''), reviewed_by, COALESCE(reviewed_by_name, ''), reviewed_at, cancelled_at, COALESCE(device_info, '{}'::jsonb), COALESCE(extra, '{}'::jsonb), created_at, updated_at`
	item, err := scanRoleApplication(r.pool.QueryRow(ctx, query, status, reason, nullableInt64(adminID), adminName, id, appID))
	if err != nil {
		return nil, err
	}
	if status == "approved" {
		if _, err := r.pool.Exec(ctx, `UPDATE user_profiles SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object('role', $1), updated_at = NOW() WHERE user_id = $2`, item.RequestedRole, item.UserID); err != nil {
			return nil, err
		}
	}
	return item, nil
}

func (r *Repository) GetRoleApplicationStatistics(ctx context.Context, appID int64) (*userdomain.RoleApplicationStatistics, error) {
	result := &userdomain.RoleApplicationStatistics{AppID: appID, ByRole: map[string]int64{}, ByPriority: map[string]int64{}}
	rows, err := r.pool.Query(ctx, `SELECT status, COUNT(*) FROM role_applications WHERE appid = $1 GROUP BY status`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result.Total += count
		switch status {
		case "pending":
			result.Pending = count
		case "approved":
			result.Approved = count
		case "rejected":
			result.Rejected = count
		case "cancelled":
			result.Cancelled = count
		}
	}
	roleRows, err := r.pool.Query(ctx, `SELECT requested_role, COUNT(*) FROM role_applications WHERE appid = $1 GROUP BY requested_role`, appID)
	if err != nil {
		return nil, err
	}
	defer roleRows.Close()
	for roleRows.Next() {
		var role string
		var count int64
		if err := roleRows.Scan(&role, &count); err != nil {
			return nil, err
		}
		result.ByRole[role] = count
	}
	priorityRows, err := r.pool.Query(ctx, `SELECT priority, COUNT(*) FROM role_applications WHERE appid = $1 GROUP BY priority`, appID)
	if err != nil {
		return nil, err
	}
	defer priorityRows.Close()
	for priorityRows.Next() {
		var priority string
		var count int64
		if err := priorityRows.Scan(&priority, &count); err != nil {
			return nil, err
		}
		result.ByPriority[priority] = count
	}
	return result, nil
}

func scanRoleDefinition(row interface{ Scan(dest ...any) error }) (*userdomain.RoleDefinition, error) {
	var item userdomain.RoleDefinition
	var raw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.RoleKey, &item.RoleName, &item.Description, &item.Priority, &item.IsEnabled, &raw, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(raw, &item.Metadata)
	return &item, nil
}

func scanRoleApplication(row interface{ Scan(dest ...any) error }) (*userdomain.RoleApplication, error) {
	var item userdomain.RoleApplication
	var deviceRaw []byte
	var extraRaw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.RequestedRole, &item.CurrentRole, &item.Reason, &item.Status, &item.Priority, &item.ValidDays, &item.ReviewReason, &item.ReviewedBy, &item.ReviewedByName, &item.ReviewedAt, &item.CancelledAt, &deviceRaw, &extraRaw, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(deviceRaw, &item.DeviceInfo)
	_ = json.Unmarshal(extraRaw, &item.Extra)
	return &item, nil
}

func scanRoleApplicationWithUser(row interface{ Scan(dest ...any) error }) (*userdomain.RoleApplication, error) {
	var item userdomain.RoleApplication
	var deviceRaw []byte
	var extraRaw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.RequestedRole, &item.CurrentRole, &item.Reason, &item.Status, &item.Priority, &item.ValidDays, &item.ReviewReason, &item.ReviewedBy, &item.ReviewedByName, &item.ReviewedAt, &item.CancelledAt, &deviceRaw, &extraRaw, &item.CreatedAt, &item.UpdatedAt, &item.Account, &item.Nickname, &item.Avatar); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(deviceRaw, &item.DeviceInfo)
	_ = json.Unmarshal(extraRaw, &item.Extra)
	return &item, nil
}

func CurrentRoleFromProfile(profile *userdomain.Profile) string {
	if profile == nil || profile.Extra == nil {
		return "user"
	}
	value, ok := profile.Extra["role"]
	if !ok || value == nil {
		return "user"
	}
	role := strings.TrimSpace(fmt.Sprintf("%v", value))
	if role == "" {
		return "user"
	}
	return role
}

func nowPtr() *time.Time {
	now := time.Now().UTC()
	return &now
}
