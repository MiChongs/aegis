package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	notificationdomain "aegis/internal/domain/notification"

	"github.com/jackc/pgx/v5"
)

// 管理员收件箱数据访问层。
// 与用户站内信（notifications）严格分表，避免 admin_id 与 user_id 主键空间混用。

const adminInboxColumns = `id, admin_id, type, title, content, level, status,
	resource, resource_id, link, COALESCE(metadata, '{}'::jsonb), created_at, read_at`

func scanAdminInboxItem(row interface{ Scan(dest ...any) error }) (*notificationdomain.AdminInboxItem, error) {
	item := &notificationdomain.AdminInboxItem{}
	var metadataRaw []byte
	if err := row.Scan(&item.ID, &item.AdminID, &item.Type, &item.Title, &item.Content,
		&item.Level, &item.Status, &item.Resource, &item.ResourceID, &item.Link,
		&metadataRaw, &item.CreatedAt, &item.ReadAt); err != nil {
		return nil, err
	}
	if len(metadataRaw) > 0 {
		_ = json.Unmarshal(metadataRaw, &item.Metadata)
	}
	return item, nil
}

// InsertAdminNotifications 批量写入管理员通知，返回实际写入条数。
//
// 走单条 INSERT ... ON CONFLICT DO NOTHING 而不是 COPY：
// 需要按 dedupe_key 逐条去重，且单次投递的收件人量级是"几个人"而非"几万人"。
func (r *Repository) InsertAdminNotifications(ctx context.Context, items []notificationdomain.AdminInboxPush) (int64, error) {
	if len(items) == 0 {
		return 0, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var inserted int64
	for _, item := range items {
		if item.AdminID <= 0 || strings.TrimSpace(item.Title) == "" {
			continue
		}
		metadataJSON, _ := json.Marshal(orEmptyMap(item.Metadata))
		var dedupe *string
		if key := strings.TrimSpace(item.DedupeKey); key != "" {
			dedupe = &key
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO admin_notifications
				(admin_id, type, title, content, level, resource, resource_id, link, dedupe_key, metadata)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING`,
			item.AdminID, item.Type, item.Title, item.Content, item.Level,
			item.Resource, item.ResourceID, item.Link, dedupe, metadataJSON)
		if err != nil {
			return 0, err
		}
		inserted += tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	committed = true
	return inserted, nil
}

// ListAdminNotifications 收件箱分页（含总数与未读数）。
func (r *Repository) ListAdminNotifications(ctx context.Context, adminID int64, query notificationdomain.AdminInboxQuery) (
	[]notificationdomain.AdminInboxItem, int64, int64, error) {

	clauses := []string{"admin_id = $1"}
	args := []any{adminID}

	if status := strings.TrimSpace(query.Status); status != "" && status != "all" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if value := strings.TrimSpace(query.Type); value != "" {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("type = $%d", len(args)))
	}
	if value := strings.TrimSpace(query.Level); value != "" {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("level = $%d", len(args)))
	}
	if value := strings.TrimSpace(query.Resource); value != "" {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("resource = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		clauses = append(clauses, fmt.Sprintf("(title ILIKE $%d OR content ILIKE $%d)", len(args), len(args)))
	}
	where := " WHERE " + strings.Join(clauses, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_notifications"+where, args...).Scan(&total); err != nil {
		return nil, 0, 0, err
	}

	// 未读数不受过滤条件影响：角标要的是"这个人一共有多少条没看"
	var unread int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_notifications WHERE admin_id = $1 AND status = 'unread'`, adminID).Scan(&unread); err != nil {
		return nil, 0, 0, err
	}
	if total == 0 {
		return []notificationdomain.AdminInboxItem{}, 0, unread, nil
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	args = append(args, limit, (page-1)*limit)
	sql := "SELECT " + adminInboxColumns + " FROM admin_notifications" + where +
		fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	items := make([]notificationdomain.AdminInboxItem, 0, limit)
	for rows.Next() {
		item, err := scanAdminInboxItem(rows)
		if err != nil {
			return nil, 0, 0, err
		}
		items = append(items, *item)
	}
	return items, total, unread, rows.Err()
}

// CountAdminUnread 未读数（角标专用，避免拉整页）。
func (r *Repository) CountAdminUnread(ctx context.Context, adminID int64) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_notifications WHERE admin_id = $1 AND status = 'unread'`, adminID).Scan(&count)
	return count, err
}

// MarkAdminNotificationsRead 标记指定通知为已读；ids 为空表示全部标记。
// 始终带 admin_id 条件，防止越权改别人的收件箱。
func (r *Repository) MarkAdminNotificationsRead(ctx context.Context, adminID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		tag, err := r.pool.Exec(ctx,
			`UPDATE admin_notifications SET status = 'read', read_at = NOW()
			 WHERE admin_id = $1 AND status = 'unread'`, adminID)
		if err != nil {
			return 0, err
		}
		return tag.RowsAffected(), nil
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE admin_notifications SET status = 'read', read_at = NOW()
		 WHERE admin_id = $1 AND id = ANY($2) AND status = 'unread'`, adminID, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteAdminNotifications 删除指定通知；ids 为空表示清空该管理员的收件箱。
func (r *Repository) DeleteAdminNotifications(ctx context.Context, adminID int64, ids []int64, onlyRead bool) (int64, error) {
	if len(ids) > 0 {
		tag, err := r.pool.Exec(ctx,
			`DELETE FROM admin_notifications WHERE admin_id = $1 AND id = ANY($2)`, adminID, ids)
		if err != nil {
			return 0, err
		}
		return tag.RowsAffected(), nil
	}
	sql := `DELETE FROM admin_notifications WHERE admin_id = $1`
	if onlyRead {
		sql += ` AND status = 'read'`
	}
	tag, err := r.pool.Exec(ctx, sql, adminID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PurgeAdminNotifications 清理 N 天前的已读通知（定时任务用）。
func (r *Repository) PurgeAdminNotifications(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		days = 90
	}
	tag, err := r.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM admin_notifications WHERE status = 'read' AND created_at < NOW() - INTERVAL '%d days'`, days))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListSuperAdminIDs 活跃的超级管理员，作为「无人认领工单」的通知兜底收件人。
func (r *Repository) ListSuperAdminIDs(ctx context.Context) ([]int64, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id FROM admin_accounts WHERE is_super_admin = TRUE AND status = 'active' ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, 4)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FilterActiveAdminIDs 过滤出仍然活跃的管理员，避免给停用账号堆积通知。
func (r *Repository) FilterActiveAdminIDs(ctx context.Context, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id FROM admin_accounts WHERE id = ANY($1) AND status = 'active'`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
