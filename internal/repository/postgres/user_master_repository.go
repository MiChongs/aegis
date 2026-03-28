package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	userdomain "aegis/internal/domain/user"

	"github.com/jackc/pgx/v5"
)

// ════════════════════════════════════════════════════════════
//  统一身份
// ════════════════════════════════════════════════════════════

// CreateGlobalIdentity 创建全局统一身份
func (r *Repository) CreateGlobalIdentity(ctx context.Context, email, phone, displayName string) (*userdomain.GlobalIdentity, error) {
	query := `INSERT INTO global_identities (email, phone, display_name, status, risk_score, risk_level, lifecycle_state, lifecycle_changed_at, metadata, created_at, updated_at)
VALUES ($1, $2, $3, 'active', 0, 'normal', 'registered', NOW(), '{}', NOW(), NOW())
RETURNING id, email, phone, display_name, status, risk_score, risk_level, lifecycle_state, lifecycle_changed_at, metadata, created_at, updated_at, deleted_at`
	return scanGlobalIdentity(r.pool.QueryRow(ctx, query, nullableString(email), nullableString(phone), displayName))
}

// GetGlobalIdentity 按 ID 获取全局身份
func (r *Repository) GetGlobalIdentity(ctx context.Context, id int64) (*userdomain.GlobalIdentity, error) {
	query := `SELECT id, email, phone, display_name, status, risk_score, risk_level, lifecycle_state, lifecycle_changed_at, metadata, created_at, updated_at, deleted_at
FROM global_identities WHERE id = $1 LIMIT 1`
	return scanGlobalIdentity(r.pool.QueryRow(ctx, query, id))
}

// FindIdentityByEmail 按邮箱查找身份
func (r *Repository) FindIdentityByEmail(ctx context.Context, email string) (*userdomain.GlobalIdentity, error) {
	query := `SELECT id, email, phone, display_name, status, risk_score, risk_level, lifecycle_state, lifecycle_changed_at, metadata, created_at, updated_at, deleted_at
FROM global_identities WHERE email = $1 AND status != 'deleted' LIMIT 1`
	return scanGlobalIdentity(r.pool.QueryRow(ctx, query, email))
}

// FindIdentityByPhone 按手机号查找身份
func (r *Repository) FindIdentityByPhone(ctx context.Context, phone string) (*userdomain.GlobalIdentity, error) {
	query := `SELECT id, email, phone, display_name, status, risk_score, risk_level, lifecycle_state, lifecycle_changed_at, metadata, created_at, updated_at, deleted_at
FROM global_identities WHERE phone = $1 AND status != 'deleted' LIMIT 1`
	return scanGlobalIdentity(r.pool.QueryRow(ctx, query, phone))
}

// ListGlobalIdentities 分页查询全局身份列表（支持关键词搜索、状态、生命周期、风险等级、标签过滤）
func (r *Repository) ListGlobalIdentities(ctx context.Context, q userdomain.IdentityListQuery) ([]userdomain.GlobalIdentity, int64, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 {
		q.Limit = 20
	}
	offset := (q.Page - 1) * q.Limit

	baseFrom := ` FROM global_identities g`
	args := make([]any, 0, 8)
	conditions := make([]string, 0, 6)

	// 标签过滤：JOIN user_tag_assignments
	if q.TagID != nil {
		baseFrom += ` JOIN user_tag_assignments ta ON ta.identity_id = g.id`
		args = append(args, *q.TagID)
		conditions = append(conditions, fmt.Sprintf("ta.tag_id = $%d", len(args)))
	}

	// 关键词搜索
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		args = append(args, like)
		p := len(args)
		conditions = append(conditions, fmt.Sprintf("(g.email ILIKE $%d OR g.phone ILIKE $%d OR g.display_name ILIKE $%d)", p, p, p))
	}

	// 状态过滤
	if q.Status != "" {
		args = append(args, q.Status)
		conditions = append(conditions, fmt.Sprintf("g.status = $%d", len(args)))
	}

	// 生命周期过滤
	if q.LifecycleState != "" {
		args = append(args, q.LifecycleState)
		conditions = append(conditions, fmt.Sprintf("g.lifecycle_state = $%d", len(args)))
	}

	// 风险等级过滤
	if q.RiskLevel != "" {
		args = append(args, q.RiskLevel)
		conditions = append(conditions, fmt.Sprintf("g.risk_level = $%d", len(args)))
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	// 计数
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT g.id)`+baseFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 查询列表
	args = append(args, q.Limit, offset)
	query := `SELECT DISTINCT g.id, g.email, g.phone, g.display_name, g.status, g.risk_score, g.risk_level, g.lifecycle_state, g.lifecycle_changed_at, g.metadata, g.created_at, g.updated_at, g.deleted_at` +
		baseFrom + where +
		fmt.Sprintf(` ORDER BY g.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]userdomain.GlobalIdentity, 0, q.Limit)
	for rows.Next() {
		gi, err := scanGlobalIdentityRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *gi)
	}
	return items, total, rows.Err()
}

// UpdateIdentityStatus 更新身份状态
func (r *Repository) UpdateIdentityStatus(ctx context.Context, id int64, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE global_identities SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	return err
}

// UpdateIdentityLifecycle 更新身份生命周期状态
func (r *Repository) UpdateIdentityLifecycle(ctx context.Context, id int64, state string) error {
	_, err := r.pool.Exec(ctx, `UPDATE global_identities SET lifecycle_state = $2, lifecycle_changed_at = NOW(), updated_at = NOW() WHERE id = $1`, id, state)
	return err
}

// UpdateIdentityRisk 更新身份风险评分
func (r *Repository) UpdateIdentityRisk(ctx context.Context, id int64, score int, level string) error {
	_, err := r.pool.Exec(ctx, `UPDATE global_identities SET risk_score = $2, risk_level = $3, updated_at = NOW() WHERE id = $1`, id, score, level)
	return err
}

// ════════════════════════════════════════════════════════════
//  跨应用用户映射
// ════════════════════════════════════════════════════════════

// CreateIdentityMapping 创建身份-用户映射
func (r *Repository) CreateIdentityMapping(ctx context.Context, identityID, appID, userID int64) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO identity_user_mappings (identity_id, app_id, user_id, created_at)
VALUES ($1, $2, $3, NOW()) ON CONFLICT DO NOTHING`, identityID, appID, userID)
	return err
}

// ListMappingsByIdentity 查询身份下所有应用映射（填充应用名、账号、昵称）
func (r *Repository) ListMappingsByIdentity(ctx context.Context, identityID int64) ([]userdomain.IdentityUserMapping, error) {
	query := `SELECT m.id, m.identity_id, m.app_id, m.user_id, m.created_at,
		COALESCE(a.name, ''), COALESCE(u.account, ''), COALESCE(p.nickname, '')
FROM identity_user_mappings m
LEFT JOIN apps a ON a.id = m.app_id
LEFT JOIN users u ON u.id = m.user_id
LEFT JOIN user_profiles p ON p.user_id = m.user_id
WHERE m.identity_id = $1
ORDER BY m.created_at ASC`
	rows, err := r.pool.Query(ctx, query, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.IdentityUserMapping, 0, 4)
	for rows.Next() {
		var m userdomain.IdentityUserMapping
		if err := rows.Scan(&m.ID, &m.IdentityID, &m.AppID, &m.UserID, &m.CreatedAt,
			&m.AppName, &m.Account, &m.Nickname); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// ListMappingsByApp 查询应用下所有映射
func (r *Repository) ListMappingsByApp(ctx context.Context, appID int64) ([]userdomain.IdentityUserMapping, error) {
	query := `SELECT id, identity_id, app_id, user_id, created_at FROM identity_user_mappings WHERE app_id = $1 ORDER BY created_at ASC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.IdentityUserMapping, 0, 8)
	for rows.Next() {
		var m userdomain.IdentityUserMapping
		if err := rows.Scan(&m.ID, &m.IdentityID, &m.AppID, &m.UserID, &m.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// DeleteIdentityMapping 删除映射
func (r *Repository) DeleteIdentityMapping(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM identity_user_mappings WHERE id = $1`, id)
	return err
}

// ════════════════════════════════════════════════════════════
//  用户标签
// ════════════════════════════════════════════════════════════

// CreateUserTag 创建用户标签
func (r *Repository) CreateUserTag(ctx context.Context, input userdomain.CreateTagInput, createdBy int64) (*userdomain.UserTag, error) {
	query := `INSERT INTO user_tags (name, color, description, created_by, created_at)
VALUES ($1, $2, $3, $4, NOW())
RETURNING id, name, color, description, created_by, created_at`
	var tag userdomain.UserTag
	if err := r.pool.QueryRow(ctx, query, input.Name, input.Color, input.Description, createdBy).Scan(
		&tag.ID, &tag.Name, &tag.Color, &tag.Description, &tag.CreatedBy, &tag.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &tag, nil
}

// ListUserTags 列出所有标签
func (r *Repository) ListUserTags(ctx context.Context) ([]userdomain.UserTag, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, color, description, created_by, created_at FROM user_tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.UserTag, 0, 16)
	for rows.Next() {
		var t userdomain.UserTag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Description, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

// DeleteUserTag 删除标签（CASCADE 会删除 assignments）
func (r *Repository) DeleteUserTag(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_tags WHERE id = $1`, id)
	return err
}

// AssignTagToIdentity 给身份分配标签
func (r *Repository) AssignTagToIdentity(ctx context.Context, identityID, tagID, assignedBy int64) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO user_tag_assignments (identity_id, tag_id, assigned_by, created_at)
VALUES ($1, $2, $3, NOW()) ON CONFLICT DO NOTHING`, identityID, tagID, assignedBy)
	return err
}

// RemoveTagFromIdentity 移除身份标签
func (r *Repository) RemoveTagFromIdentity(ctx context.Context, identityID, tagID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_tag_assignments WHERE identity_id = $1 AND tag_id = $2`, identityID, tagID)
	return err
}

// ListIdentityTags 获取身份的所有标签
func (r *Repository) ListIdentityTags(ctx context.Context, identityID int64) ([]userdomain.UserTag, error) {
	query := `SELECT t.id, t.name, t.color, t.description, t.created_by, t.created_at
FROM user_tags t
JOIN user_tag_assignments a ON a.tag_id = t.id
WHERE a.identity_id = $1
ORDER BY t.name`
	rows, err := r.pool.Query(ctx, query, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.UserTag, 0, 8)
	for rows.Next() {
		var t userdomain.UserTag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Description, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

// ListIdentitiesByTag 查询拥有某标签的所有身份
func (r *Repository) ListIdentitiesByTag(ctx context.Context, tagID int64) ([]userdomain.GlobalIdentity, error) {
	query := `SELECT g.id, g.email, g.phone, g.display_name, g.status, g.risk_score, g.risk_level, g.lifecycle_state, g.lifecycle_changed_at, g.metadata, g.created_at, g.updated_at, g.deleted_at
FROM global_identities g
JOIN user_tag_assignments a ON a.identity_id = g.id
WHERE a.tag_id = $1
ORDER BY g.id`
	rows, err := r.pool.Query(ctx, query, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.GlobalIdentity, 0, 16)
	for rows.Next() {
		gi, err := scanGlobalIdentityRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *gi)
	}
	return items, rows.Err()
}

// ════════════════════════════════════════════════════════════
//  用户分群
// ════════════════════════════════════════════════════════════

// CreateUserSegment 创建用户分群
func (r *Repository) CreateUserSegment(ctx context.Context, input userdomain.CreateSegmentInput, createdBy int64) (*userdomain.UserSegment, error) {
	rulesJSON, _ := json.Marshal(input.Rules)
	if input.Rules == nil {
		rulesJSON = []byte("{}")
	}
	query := `INSERT INTO user_segments (name, description, segment_type, rules, member_count, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, 0, $5, NOW(), NOW())
RETURNING id, name, description, segment_type, rules, member_count, created_by, created_at, updated_at`
	var seg userdomain.UserSegment
	var rulesRaw []byte
	if err := r.pool.QueryRow(ctx, query, input.Name, input.Description, input.SegmentType, rulesJSON, createdBy).Scan(
		&seg.ID, &seg.Name, &seg.Description, &seg.SegmentType, &rulesRaw, &seg.MemberCount, &seg.CreatedBy, &seg.CreatedAt, &seg.UpdatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(rulesRaw, &seg.Rules)
	return &seg, nil
}

// ListUserSegments 列出所有分群
func (r *Repository) ListUserSegments(ctx context.Context) ([]userdomain.UserSegment, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, description, segment_type, rules, member_count, created_by, created_at, updated_at FROM user_segments ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.UserSegment, 0, 16)
	for rows.Next() {
		var seg userdomain.UserSegment
		var rulesRaw []byte
		if err := rows.Scan(&seg.ID, &seg.Name, &seg.Description, &seg.SegmentType, &rulesRaw, &seg.MemberCount, &seg.CreatedBy, &seg.CreatedAt, &seg.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rulesRaw, &seg.Rules)
		items = append(items, seg)
	}
	return items, rows.Err()
}

// UpdateUserSegment 更新分群
func (r *Repository) UpdateUserSegment(ctx context.Context, id int64, name, desc string, rules map[string]any) error {
	rulesJSON, _ := json.Marshal(rules)
	if rules == nil {
		rulesJSON = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `UPDATE user_segments SET name = $2, description = $3, rules = $4, updated_at = NOW() WHERE id = $1`,
		id, name, desc, rulesJSON)
	return err
}

// DeleteUserSegment 删除分群（CASCADE 删除成员）
func (r *Repository) DeleteUserSegment(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_segments WHERE id = $1`, id)
	return err
}

// AddSegmentMember 添加分群成员
func (r *Repository) AddSegmentMember(ctx context.Context, segmentID, identityID, addedBy int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `INSERT INTO user_segment_members (segment_id, identity_id, added_by, created_at)
VALUES ($1, $2, $3, NOW()) ON CONFLICT DO NOTHING`, segmentID, identityID, addedBy)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE user_segments SET member_count = (SELECT COUNT(*) FROM user_segment_members WHERE segment_id = $1), updated_at = NOW() WHERE id = $1`, segmentID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveSegmentMember 移除分群成员
func (r *Repository) RemoveSegmentMember(ctx context.Context, segmentID, identityID int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `DELETE FROM user_segment_members WHERE segment_id = $1 AND identity_id = $2`, segmentID, identityID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE user_segments SET member_count = (SELECT COUNT(*) FROM user_segment_members WHERE segment_id = $1), updated_at = NOW() WHERE id = $1`, segmentID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListSegmentMembers 分页查询分群成员
func (r *Repository) ListSegmentMembers(ctx context.Context, segmentID int64, page, limit int) ([]userdomain.GlobalIdentity, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_segment_members WHERE segment_id = $1`, segmentID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT g.id, g.email, g.phone, g.display_name, g.status, g.risk_score, g.risk_level, g.lifecycle_state, g.lifecycle_changed_at, g.metadata, g.created_at, g.updated_at, g.deleted_at
FROM global_identities g
JOIN user_segment_members m ON m.identity_id = g.id
WHERE m.segment_id = $1
ORDER BY m.created_at DESC
LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, query, segmentID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]userdomain.GlobalIdentity, 0, limit)
	for rows.Next() {
		gi, err := scanGlobalIdentityRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *gi)
	}
	return items, total, rows.Err()
}

// ════════════════════════════════════════════════════════════
//  黑白名单
// ════════════════════════════════════════════════════════════

// CreateUserListEntry 创建黑白名单条目
func (r *Repository) CreateUserListEntry(ctx context.Context, input userdomain.CreateListEntryInput, createdBy int64) (*userdomain.UserListEntry, error) {
	query := `INSERT INTO user_lists (list_type, identity_id, email, phone, ip, reason, expires_at, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
RETURNING id, list_type, identity_id, COALESCE(email,''), COALESCE(phone,''), COALESCE(ip,''), reason, expires_at, created_by, created_at`
	var entry userdomain.UserListEntry
	if err := r.pool.QueryRow(ctx, query,
		input.ListType, input.IdentityID, nullableString(input.Email), nullableString(input.Phone), nullableString(input.IP),
		input.Reason, input.ExpiresAt, createdBy,
	).Scan(
		&entry.ID, &entry.ListType, &entry.IdentityID, &entry.Email, &entry.Phone, &entry.IP,
		&entry.Reason, &entry.ExpiresAt, &entry.CreatedBy, &entry.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &entry, nil
}

// ListUserListEntries 分页查询黑白名单
func (r *Repository) ListUserListEntries(ctx context.Context, listType string, page, limit int) ([]userdomain.UserListEntry, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	baseWhere := ""
	args := make([]any, 0, 3)
	if listType != "" {
		args = append(args, listType)
		baseWhere = fmt.Sprintf(" WHERE list_type = $%d", len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_lists`+baseWhere, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	query := `SELECT id, list_type, identity_id, COALESCE(email,''), COALESCE(phone,''), COALESCE(ip,''), reason, expires_at, created_by, created_at
FROM user_lists` + baseWhere + fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]userdomain.UserListEntry, 0, limit)
	for rows.Next() {
		var entry userdomain.UserListEntry
		if err := rows.Scan(&entry.ID, &entry.ListType, &entry.IdentityID, &entry.Email, &entry.Phone, &entry.IP,
			&entry.Reason, &entry.ExpiresAt, &entry.CreatedBy, &entry.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, entry)
	}
	return items, total, rows.Err()
}

// DeleteUserListEntry 删除黑白名单条目
func (r *Repository) DeleteUserListEntry(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_lists WHERE id = $1`, id)
	return err
}

// CheckBlacklisted 检查邮箱/手机/IP 是否在黑名单中
func (r *Repository) CheckBlacklisted(ctx context.Context, email, phone, ip string) (bool, error) {
	conditions := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if email != "" {
		args = append(args, email)
		conditions = append(conditions, fmt.Sprintf("email = $%d", len(args)))
	}
	if phone != "" {
		args = append(args, phone)
		conditions = append(conditions, fmt.Sprintf("phone = $%d", len(args)))
	}
	if ip != "" {
		args = append(args, ip)
		conditions = append(conditions, fmt.Sprintf("ip = $%d", len(args)))
	}
	if len(conditions) == 0 {
		return false, nil
	}
	query := `SELECT EXISTS(SELECT 1 FROM user_lists WHERE list_type = 'blacklist' AND (` + strings.Join(conditions, " OR ") + `) AND (expires_at IS NULL OR expires_at > NOW()))`
	var exists bool
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// ════════════════════════════════════════════════════════════
//  账号合并
// ════════════════════════════════════════════════════════════

// ExecuteIdentityMerge 执行身份合并（事务：迁移映射 + 标记删除 + 记录合并）
func (r *Repository) ExecuteIdentityMerge(ctx context.Context, primaryID, mergedID, mergedBy int64) (*userdomain.IdentityMerge, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 迁移映射到主身份
	_, err = tx.Exec(ctx, `UPDATE identity_user_mappings SET identity_id = $1 WHERE identity_id = $2`, primaryID, mergedID)
	if err != nil {
		return nil, err
	}
	// 标记被合并身份为已删除
	_, err = tx.Exec(ctx, `UPDATE global_identities SET status = 'deleted', updated_at = NOW(), deleted_at = NOW() WHERE id = $1`, mergedID)
	if err != nil {
		return nil, err
	}
	// 迁移标签分配（忽略冲突）
	_, err = tx.Exec(ctx, `UPDATE user_tag_assignments SET identity_id = $1 WHERE identity_id = $2 AND tag_id NOT IN (SELECT tag_id FROM user_tag_assignments WHERE identity_id = $1)`, primaryID, mergedID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `DELETE FROM user_tag_assignments WHERE identity_id = $1`, mergedID)
	if err != nil {
		return nil, err
	}
	// 记录合并
	query := `INSERT INTO identity_merges (primary_id, merged_id, merged_by, status, details, created_at)
VALUES ($1, $2, $3, 'completed', '{}', NOW())
RETURNING id, primary_id, merged_id, merged_by, status, details, created_at`
	var merge userdomain.IdentityMerge
	var detailsRaw []byte
	if err := tx.QueryRow(ctx, query, primaryID, mergedID, mergedBy).Scan(
		&merge.ID, &merge.PrimaryID, &merge.MergedID, &merge.MergedBy, &merge.Status, &detailsRaw, &merge.CreatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(detailsRaw, &merge.Details)

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &merge, nil
}

// ListIdentityMerges 列出所有合并记录
func (r *Repository) ListIdentityMerges(ctx context.Context) ([]userdomain.IdentityMerge, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, primary_id, merged_id, merged_by, status, details, created_at FROM identity_merges ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.IdentityMerge, 0, 16)
	for rows.Next() {
		var m userdomain.IdentityMerge
		var detailsRaw []byte
		if err := rows.Scan(&m.ID, &m.PrimaryID, &m.MergedID, &m.MergedBy, &m.Status, &detailsRaw, &m.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(detailsRaw, &m.Details)
		items = append(items, m)
	}
	return items, rows.Err()
}

// ════════════════════════════════════════════════════════════
//  用户申诉
// ════════════════════════════════════════════════════════════

// CreateUserAppeal 创建用户申诉
func (r *Repository) CreateUserAppeal(ctx context.Context, identityID int64, input userdomain.CreateAppealInput) (*userdomain.UserAppeal, error) {
	query := `INSERT INTO user_appeals (identity_id, appeal_type, reason, evidence, status, created_at)
VALUES ($1, $2, $3, $4, 'pending', NOW())
RETURNING id, identity_id, appeal_type, reason, evidence, status, reviewer_id, review_comment, reviewed_at, created_at`
	var a userdomain.UserAppeal
	if err := r.pool.QueryRow(ctx, query, identityID, input.AppealType, input.Reason, input.Evidence).Scan(
		&a.ID, &a.IdentityID, &a.AppealType, &a.Reason, &a.Evidence, &a.Status,
		&a.ReviewerID, &a.ReviewComment, &a.ReviewedAt, &a.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

// ListUserAppeals 分页查询申诉列表
func (r *Repository) ListUserAppeals(ctx context.Context, status string, page, limit int) ([]userdomain.UserAppeal, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	baseFrom := ` FROM user_appeals a LEFT JOIN global_identities g ON g.id = a.identity_id`
	args := make([]any, 0, 3)
	where := ""
	if status != "" {
		args = append(args, status)
		where = fmt.Sprintf(" WHERE a.status = $%d", len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+baseFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	query := `SELECT a.id, a.identity_id, a.appeal_type, a.reason, a.evidence, a.status,
		a.reviewer_id, a.review_comment, a.reviewed_at, a.created_at, COALESCE(g.display_name, '')` +
		baseFrom + where + fmt.Sprintf(` ORDER BY a.created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]userdomain.UserAppeal, 0, limit)
	for rows.Next() {
		var a userdomain.UserAppeal
		if err := rows.Scan(&a.ID, &a.IdentityID, &a.AppealType, &a.Reason, &a.Evidence, &a.Status,
			&a.ReviewerID, &a.ReviewComment, &a.ReviewedAt, &a.CreatedAt, &a.IdentityName); err != nil {
			return nil, 0, err
		}
		items = append(items, a)
	}
	return items, total, rows.Err()
}

// ReviewUserAppeal 审核申诉
func (r *Repository) ReviewUserAppeal(ctx context.Context, id, reviewerID int64, input userdomain.ReviewAppealInput) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_appeals SET status = $2, reviewer_id = $3, review_comment = $4, reviewed_at = NOW() WHERE id = $1`,
		id, input.Action, reviewerID, input.Comment)
	return err
}

// ════════════════════════════════════════════════════════════
//  注销请求
// ════════════════════════════════════════════════════════════

// CreateDeactivationRequest 创建注销请求
func (r *Repository) CreateDeactivationRequest(ctx context.Context, identityID int64, reason string, coolingDays int) (*userdomain.DeactivationRequest, error) {
	query := `INSERT INTO deactivation_requests (identity_id, reason, cooling_days, scheduled_at, status, created_at)
VALUES ($1, $2, $3, NOW() + ($3 || ' days')::INTERVAL, 'pending', NOW())
RETURNING id, identity_id, reason, cooling_days, scheduled_at, status, created_at`
	var d userdomain.DeactivationRequest
	if err := r.pool.QueryRow(ctx, query, identityID, reason, coolingDays).Scan(
		&d.ID, &d.IdentityID, &d.Reason, &d.CoolingDays, &d.ScheduledAt, &d.Status, &d.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &d, nil
}

// CancelDeactivation 取消注销请求
func (r *Repository) CancelDeactivation(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE deactivation_requests SET status = 'cancelled' WHERE id = $1`, id)
	return err
}

// ListPendingDeactivations 列出待处理的注销请求
func (r *Repository) ListPendingDeactivations(ctx context.Context) ([]userdomain.DeactivationRequest, error) {
	query := `SELECT d.id, d.identity_id, d.reason, d.cooling_days, d.scheduled_at, d.status, d.created_at, COALESCE(g.display_name, '')
FROM deactivation_requests d
LEFT JOIN global_identities g ON g.id = d.identity_id
WHERE d.status = 'pending'
ORDER BY d.scheduled_at ASC`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.DeactivationRequest, 0, 8)
	for rows.Next() {
		var d userdomain.DeactivationRequest
		if err := rows.Scan(&d.ID, &d.IdentityID, &d.Reason, &d.CoolingDays, &d.ScheduledAt, &d.Status, &d.CreatedAt, &d.IdentityName); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

// ════════════════════════════════════════════════════════════
//  辅助 scan 函数
// ════════════════════════════════════════════════════════════

// scanGlobalIdentity 从单行扫描 GlobalIdentity（用于 QueryRow）
func scanGlobalIdentity(row pgx.Row) (*userdomain.GlobalIdentity, error) {
	var gi userdomain.GlobalIdentity
	var metaRaw []byte
	var email sql.NullString
	var phone sql.NullString
	var displayName sql.NullString
	if err := row.Scan(
		&gi.ID, &email, &phone, &displayName, &gi.Status,
		&gi.RiskScore, &gi.RiskLevel, &gi.LifecycleState, &gi.LifecycleChangedAt,
		&metaRaw, &gi.CreatedAt, &gi.UpdatedAt, &gi.DeletedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if email.Valid {
		gi.Email = email.String
	}
	if phone.Valid {
		gi.Phone = phone.String
	}
	if displayName.Valid {
		gi.DisplayName = displayName.String
	}
	_ = json.Unmarshal(metaRaw, &gi.Metadata)
	return &gi, nil
}

// scanGlobalIdentityRow 从 rows 扫描 GlobalIdentity（用于 Query 多行）
func scanGlobalIdentityRow(rows pgx.Rows) (*userdomain.GlobalIdentity, error) {
	var gi userdomain.GlobalIdentity
	var metaRaw []byte
	var email sql.NullString
	var phone sql.NullString
	var displayName sql.NullString
	if err := rows.Scan(
		&gi.ID, &email, &phone, &displayName, &gi.Status,
		&gi.RiskScore, &gi.RiskLevel, &gi.LifecycleState, &gi.LifecycleChangedAt,
		&metaRaw, &gi.CreatedAt, &gi.UpdatedAt, &gi.DeletedAt,
	); err != nil {
		return nil, err
	}
	if email.Valid {
		gi.Email = email.String
	}
	if phone.Valid {
		gi.Phone = phone.String
	}
	if displayName.Valid {
		gi.DisplayName = displayName.String
	}
	_ = json.Unmarshal(metaRaw, &gi.Metadata)
	return &gi, nil
}

// ListAllAppUsers 查询某应用下所有用户 ID/email/phone（用于批量同步身份）
func (r *Repository) ListAllAppUsers(ctx context.Context, appID int64) ([]struct {
	UserID  int64
	Account string
	Email   string
	Phone   string
}, error) {
	query := `SELECT u.id, u.account, COALESCE(p.email,''), COALESCE(p.phone,'')
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1
ORDER BY u.id ASC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type appUser struct {
		UserID  int64
		Account string
		Email   string
		Phone   string
	}
	items := make([]struct {
		UserID  int64
		Account string
		Email   string
		Phone   string
	}, 0, 64)
	for rows.Next() {
		var u struct {
			UserID  int64
			Account string
			Email   string
			Phone   string
		}
		if err := rows.Scan(&u.UserID, &u.Account, &u.Email, &u.Phone); err != nil {
			return nil, err
		}
		items = append(items, u)
	}
	return items, rows.Err()
}
