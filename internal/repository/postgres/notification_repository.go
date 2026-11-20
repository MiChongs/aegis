package postgres

// 用户站内信（应用用户的消息中心）。规则见 docs/notification-center.md。
//
// 一条消息 = notifications 一行 + notification_receipts 一行（回执里存已读状态）。
// 用户侧的每条查询都同时按 n.appid、n.user_id、nr.user_id 收口：
// 读、改、删别人的消息与「不存在」是同一个结果，不会因为猜中 ID 而越权。
// 过期（expires_at <= NOW()）的消息对用户侧一律不存在：不列出、不计数、读写都按不存在处理。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	notificationdomain "aegis/internal/domain/notification"

	"github.com/jackc/pgx/v5"
)

// notificationLive 未过期。
const notificationLive = `(n.expires_at IS NULL OR n.expires_at > NOW())`

// notificationItemColumns scanNotification 读取的列，顺序与 Scan 一一对应。
// 回执里的 delivered 对外一律叫 unread。
const notificationItemColumns = `n.id, n.appid, n.user_id, n.type, n.category, n.title, n.content, n.level,
	CASE WHEN nr.status = 'read' THEN 'read' ELSE 'unread' END, n.action, n.image, n.expires_at,
	COALESCE(n.metadata, '{}'::jsonb), nr.read_at, n.created_at, n.updated_at`

// userNotificationScope 用户侧的 FROM / WHERE 骨架：$1 = appid，$2 = user_id。
const userNotificationScope = ` FROM notifications n
JOIN notification_receipts nr ON nr.notification_id = n.id AND nr.user_id = $2
WHERE n.appid = $1 AND n.user_id = $2 AND ` + notificationLive

// CreateUserNotification 落一条站内信（消息 + 回执同一事务）并返回完整一行。
//
// 只允许 NotificationService.Deliver 调用：其它写入点直接落库会绕过分类、实时推送与缓存失效，
// TestOnlyNotificationServiceCreatesUserNotifications 钉住这一点。
func (r *Repository) CreateUserNotification(ctx context.Context, in notificationdomain.DeliverInput) (*notificationdomain.Item, error) {
	metadata := in.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	var actionJSON []byte
	if in.Action != nil {
		if actionJSON, err = json.Marshal(in.Action); err != nil {
			return nil, err
		}
	}
	// 一条语句写消息与回执并带回完整一行：单条语句天然原子，比「开事务 → 两次 INSERT → 回查 → 提交」少四次往返
	return scanNotification(r.pool.QueryRow(ctx, `WITH n AS (
    INSERT INTO notifications (appid, user_id, type, category, title, content, level, status,
        metadata, action, image, expires_at, created_at, updated_at)
    VALUES ($1, $2, $3, $4, $5, $6, $7, 'unread', $8, $9, NULLIF($10, ''), $11, NOW(), NOW())
    RETURNING *
), nr AS (
    INSERT INTO notification_receipts (notification_id, user_id, status, created_at, updated_at)
    SELECT n.id, n.user_id, 'delivered', NOW(), NOW() FROM n
    RETURNING status, read_at
)
SELECT `+notificationItemColumns+` FROM n, nr`,
		in.AppID, in.UserID, in.Type, in.Category, in.Title, in.Content, in.Level, metadataJSON, actionJSON, in.Image, in.ExpiresAt))
}

// ListUserNotifications 用户的消息列表（已排除过期）。nextExpiry 是筛选范围内最早的过期时间，供缓存定寿命。
func (r *Repository) ListUserNotifications(ctx context.Context, appID int64, userID int64, query notificationdomain.UserListQuery) ([]notificationdomain.Item, int64, *time.Time, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	baseQuery, args := buildUserNotificationFilter(appID, userID, query)
	var total int64
	var nextExpiry *time.Time
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*), MIN(n.expires_at)`+baseQuery, args...).Scan(&total, &nextExpiry); err != nil {
		return nil, 0, nil, err
	}

	sql := `SELECT ` + notificationItemColumns + baseQuery + fmt.Sprintf(`
ORDER BY n.created_at DESC, n.id DESC
LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, nil, err
	}
	defer rows.Close()
	items := make([]notificationdomain.Item, 0, limit)
	for rows.Next() {
		item, err := scanNotification(rows)
		if err != nil {
			return nil, 0, nil, err
		}
		items = append(items, *item)
	}
	return items, total, nextExpiry, rows.Err()
}

// GetUserNotification 单条消息；不存在、不属于本人或已过期都返回 nil。
func (r *Repository) GetUserNotification(ctx context.Context, appID int64, userID int64, notificationID int64) (*notificationdomain.Item, error) {
	item, err := scanNotification(r.pool.QueryRow(ctx, `SELECT `+notificationItemColumns+userNotificationScope+` AND n.id = $3`,
		appID, userID, notificationID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return item, err
}

// CountUnreadNotifications 总未读（不含过期）。
func (r *Repository) CountUnreadNotifications(ctx context.Context, appID int64, userID int64) (int64, error) {
	var unread int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+userNotificationScope+` AND nr.status <> 'read'`, appID, userID).Scan(&unread)
	return unread, err
}

// NotificationUnreadCounts 总未读与按分类未读（只含 n > 0 的分类），以及未读里最早的过期时间。
func (r *Repository) NotificationUnreadCounts(ctx context.Context, appID int64, userID int64) (notificationdomain.Counts, error) {
	counts := notificationdomain.Counts{CategoryUnread: map[string]int64{}}
	rows, err := r.pool.Query(ctx, `SELECT n.category, COUNT(*), MIN(n.expires_at)`+userNotificationScope+`
  AND nr.status <> 'read'
GROUP BY n.category`, appID, userID)
	if err != nil {
		return counts, err
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		var n int64
		var expiry *time.Time
		if err := rows.Scan(&category, &n, &expiry); err != nil {
			return counts, err
		}
		if n <= 0 {
			continue
		}
		counts.CategoryUnread[category] = n
		counts.Unread += n
		if expiry != nil && (counts.NextExpiry == nil || expiry.Before(*counts.NextExpiry)) {
			value := *expiry
			counts.NextExpiry = &value
		}
	}
	return counts, rows.Err()
}

// NotificationCategoryStat 一个分类的总数与未读。
type NotificationCategoryStat struct {
	Total  int64
	Unread int64
}

// NotificationCategoryStats 按分类的总数与未读（不含过期），外加每个分类最新的一条。
func (r *Repository) NotificationCategoryStats(ctx context.Context, appID int64, userID int64) (map[string]NotificationCategoryStat, map[string]notificationdomain.Item, error) {
	stats := map[string]NotificationCategoryStat{}
	rows, err := r.pool.Query(ctx, `SELECT n.category, COUNT(*), COUNT(*) FILTER (WHERE nr.status <> 'read')`+userNotificationScope+`
GROUP BY n.category`, appID, userID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var category string
		var stat NotificationCategoryStat
		if err := rows.Scan(&category, &stat.Total, &stat.Unread); err != nil {
			rows.Close()
			return nil, nil, err
		}
		stats[category] = stat
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	latest := map[string]notificationdomain.Item{}
	rows, err = r.pool.Query(ctx, `SELECT DISTINCT ON (n.category) `+notificationItemColumns+userNotificationScope+`
ORDER BY n.category, n.created_at DESC, n.id DESC`, appID, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanNotification(rows)
		if err != nil {
			return nil, nil, err
		}
		latest[item.Category] = *item
	}
	return stats, latest, rows.Err()
}

// SetUserNotificationRead 标为已读（read=true）或未读。found=false 表示不存在、不属于本人或已过期。
// 已读的再标已读不改 read_at。
func (r *Repository) SetUserNotificationRead(ctx context.Context, appID int64, userID int64, notificationID int64, read bool) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE notification_receipts nr
SET status = CASE WHEN $4 THEN 'read' ELSE 'delivered' END,
    read_at = CASE WHEN $4 THEN COALESCE(nr.read_at, NOW()) ELSE NULL END,
    updated_at = NOW()
FROM notifications n
WHERE nr.notification_id = n.id
  AND nr.user_id = $2
  AND n.appid = $1
  AND n.user_id = $2
  AND n.id = $3
  AND `+notificationLive, appID, userID, notificationID, read)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// MarkNotificationsRead 批量标为已读，返回真正从未读变成已读的 ID。
func (r *Repository) MarkNotificationsRead(ctx context.Context, appID int64, userID int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	return r.collectIDs(ctx, `UPDATE notification_receipts nr
SET status = 'read', read_at = NOW(), updated_at = NOW()
FROM notifications n
WHERE nr.notification_id = n.id
  AND nr.user_id = $2
  AND n.appid = $1
  AND n.user_id = $2
  AND n.id = ANY($3)
  AND nr.status <> 'read'
  AND `+notificationLive+`
RETURNING n.id`, appID, userID, ids)
}

// MarkAllNotificationsRead 全部（或某一分类）标为已读，返回更新条数。
func (r *Repository) MarkAllNotificationsRead(ctx context.Context, appID int64, userID int64, category string) (int64, error) {
	args := []any{appID, userID}
	query := `UPDATE notification_receipts nr
SET status = 'read', read_at = NOW(), updated_at = NOW()
FROM notifications n
WHERE nr.notification_id = n.id
  AND nr.user_id = $2
  AND n.appid = $1
  AND n.user_id = $2
  AND nr.status <> 'read'
  AND ` + notificationLive
	if category != "" {
		args = append(args, category)
		query += fmt.Sprintf(" AND n.category = $%d", len(args))
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteUserNotification 删一条（已过期的按不存在处理）。
func (r *Repository) DeleteUserNotification(ctx context.Context, appID int64, userID int64, notificationID int64) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM notifications n
WHERE n.appid = $1 AND n.user_id = $2 AND n.id = $3 AND `+notificationLive, appID, userID, notificationID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteUserNotificationsByIDs 批量删除，返回真正删掉的 ID（别人的、过期的不算）。
func (r *Repository) DeleteUserNotificationsByIDs(ctx context.Context, appID int64, userID int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	return r.collectIDs(ctx, `DELETE FROM notifications n
WHERE n.appid = $1 AND n.user_id = $2 AND n.id = ANY($3) AND `+notificationLive+`
RETURNING n.id`, appID, userID, ids)
}

// DeleteUserNotifications 按条件清空。status：all | read | unread；category / type / level 为空即不限。
// 已过期的消息对用户不可见，清空时一并删掉（不计入返回的条数）。
func (r *Repository) DeleteUserNotifications(ctx context.Context, appID int64, userID int64, status string, category string, notificationType string, level string) (int64, error) {
	args := []any{appID, userID}
	filter := ""
	switch strings.TrimSpace(status) {
	case "read":
		filter += " AND nr.status = 'read'"
	case "unread":
		filter += " AND nr.status <> 'read'"
	}
	if category = strings.TrimSpace(category); category != "" && category != "all" {
		args = append(args, category)
		filter += fmt.Sprintf(" AND n.category = $%d", len(args))
	}
	if notificationType = strings.TrimSpace(notificationType); notificationType != "" && notificationType != "all" {
		args = append(args, notificationType)
		filter += fmt.Sprintf(" AND n.type = $%d", len(args))
	}
	if level = strings.TrimSpace(level); level != "" && level != "all" {
		args = append(args, level)
		filter += fmt.Sprintf(" AND n.level = $%d", len(args))
	}
	var deleted int64
	err := r.pool.QueryRow(ctx, `WITH removed AS (
    DELETE FROM notifications n
    USING notification_receipts nr
    WHERE n.id = nr.notification_id
      AND n.appid = $1
      AND n.user_id = $2
      AND nr.user_id = $2`+filter+`
    RETURNING `+notificationLive+` AS live
)
SELECT COUNT(*) FILTER (WHERE live) FROM removed`, args...).Scan(&deleted)
	return deleted, err
}

// GetNotificationPreferences 用户写过的提醒开关；没有行的分类即开启。
func (r *Repository) GetNotificationPreferences(ctx context.Context, appID int64, userID int64) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT category, push FROM notification_preferences WHERE appid = $1 AND user_id = $2`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prefs := map[string]bool{}
	for rows.Next() {
		var category string
		var push bool
		if err := rows.Scan(&category, &push); err != nil {
			return nil, err
		}
		prefs[category] = push
	}
	return prefs, rows.Err()
}

// SaveNotificationPreferences 写提醒开关（逐个分类 upsert，同一事务）。
func (r *Repository) SaveNotificationPreferences(ctx context.Context, appID int64, userID int64, prefs map[string]bool) error {
	if len(prefs) == 0 {
		return nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for category, push := range prefs {
		if _, err := tx.Exec(ctx, `INSERT INTO notification_preferences (appid, user_id, category, push, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (appid, user_id, category) DO UPDATE SET push = EXCLUDED.push, updated_at = NOW()`,
			appID, userID, category, push); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) collectIDs(ctx context.Context, sql string, args ...any) ([]int64, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func buildUserNotificationFilter(appID int64, userID int64, query notificationdomain.UserListQuery) (string, []any) {
	baseQuery := userNotificationScope
	args := []any{appID, userID}
	switch strings.TrimSpace(query.Status) {
	case "read":
		baseQuery += " AND nr.status = 'read'"
	case "unread":
		baseQuery += " AND nr.status <> 'read'"
	}
	if category := strings.TrimSpace(query.Category); category != "" && category != "all" {
		args = append(args, category)
		baseQuery += fmt.Sprintf(" AND n.category = $%d", len(args))
	}
	if notificationType := strings.TrimSpace(query.Type); notificationType != "" && notificationType != "all" {
		args = append(args, notificationType)
		baseQuery += fmt.Sprintf(" AND n.type = $%d", len(args))
	}
	if level := strings.TrimSpace(query.Level); level != "" && level != "all" {
		args = append(args, level)
		baseQuery += fmt.Sprintf(" AND n.level = $%d", len(args))
	}
	return baseQuery, args
}

func scanNotification(row interface{ Scan(dest ...any) error }) (*notificationdomain.Item, error) {
	var item notificationdomain.Item
	var metadata, action []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Type, &item.Category, &item.Title, &item.Content, &item.Level,
		&item.Status, &action, &item.Image, &item.ExpiresAt, &metadata, &item.ReadAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.CategoryLabel = notificationdomain.CategoryLabel(item.Category)
	item.Read = item.Status == "read"
	item.Action = decodeNotificationAction(action)
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

func decodeNotificationAction(raw []byte) *notificationdomain.Action {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var action notificationdomain.Action
	if err := json.Unmarshal(raw, &action); err != nil || action.Type == "" {
		return nil
	}
	return &action
}

// ── 管理端 ──

// adminNotificationColumns scanAdminNotification 读取的列。
const adminNotificationColumns = `n.id, n.appid, n.user_id, n.campaign_id, COALESCE(u.account, ''), COALESCE(p.nickname, ''), n.type, n.category,
	n.title, n.content, n.level, COALESCE(nr.status, n.status), n.action, n.image, n.expires_at,
	COALESCE(n.metadata, '{}'::jsonb), nr.read_at, n.created_at, n.updated_at`

func (r *Repository) ListAppNotifications(ctx context.Context, appID int64, query notificationdomain.AdminListQuery) ([]notificationdomain.AdminItem, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	filter := query.Filter()
	// 计数只连筛选真正用到的表：不按关键词时不连用户与资料表，不按已读状态时不连回执
	countQuery, countArgs := buildAdminNotificationFilter(appID, filter, false)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	baseQuery, args := buildAdminNotificationFilter(appID, filter, true)
	sql := `SELECT ` + adminNotificationColumns + baseQuery + fmt.Sprintf(`
ORDER BY n.created_at DESC, n.id DESC
LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	items, err := r.queryAdminNotifications(ctx, limit, sql, args...)
	return items, total, err
}

func (r *Repository) ListAppNotificationsForExport(ctx context.Context, appID int64, query notificationdomain.AdminExportQuery) ([]notificationdomain.AdminItem, error) {
	limit := query.Limit
	if limit < 1 {
		limit = 5000
	}
	baseQuery, args := buildAdminNotificationFilter(appID, query, true)
	sql := `SELECT ` + adminNotificationColumns + baseQuery + fmt.Sprintf(`
ORDER BY n.created_at DESC, n.id DESC
LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	return r.queryAdminNotifications(ctx, limit, sql, args...)
}

func (r *Repository) queryAdminNotifications(ctx context.Context, capacity int, sql string, args ...any) ([]notificationdomain.AdminItem, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notificationdomain.AdminItem, 0, min(capacity, 1000))
	for rows.Next() {
		item, err := scanAdminNotification(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ResolveAppNotificationIDs(ctx context.Context, appID int64, query notificationdomain.AdminExportQuery) ([]int64, error) {
	limit := query.Limit
	if limit < 1 {
		limit = 5000
	}
	baseQuery, args := buildAdminNotificationFilter(appID, query, false)
	sql := `SELECT n.id` + baseQuery + fmt.Sprintf(`
ORDER BY n.created_at DESC, n.id DESC
LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	return r.collectIDs(ctx, sql, args...)
}

// AppNotificationOwner 一条被管理员删除的消息归属谁（用于失效缓存与推送 notification.deleted）。
type AppNotificationOwner struct {
	UserID int64
	IDs    []int64
}

// DeleteAppNotifications 管理员按 ID 删除，按用户分组返回真正删掉的 ID。
func (r *Repository) DeleteAppNotifications(ctx context.Context, appID int64, ids []int64) (int64, []AppNotificationOwner, error) {
	if len(ids) == 0 {
		return 0, nil, nil
	}
	rows, err := r.pool.Query(ctx, `DELETE FROM notifications WHERE appid = $1 AND id = ANY($2) RETURNING id, user_id`, appID, ids)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var deleted int64
	byUser := map[int64]*AppNotificationOwner{}
	order := []int64{}
	for rows.Next() {
		var id int64
		var userID *int64
		if err := rows.Scan(&id, &userID); err != nil {
			return 0, nil, err
		}
		deleted++
		if userID == nil {
			continue
		}
		owner, ok := byUser[*userID]
		if !ok {
			owner = &AppNotificationOwner{UserID: *userID}
			byUser[*userID] = owner
			order = append(order, *userID)
		}
		owner.IDs = append(owner.IDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	owners := make([]AppNotificationOwner, 0, len(order))
	for _, userID := range order {
		owners = append(owners, *byUser[userID])
	}
	return deleted, owners, nil
}

func scanAdminNotification(row interface{ Scan(dest ...any) error }) (*notificationdomain.AdminItem, error) {
	var item notificationdomain.AdminItem
	var metadata, action []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.CampaignID, &item.Account, &item.Nickname, &item.Type, &item.Category,
		&item.Title, &item.Content, &item.Level, &item.Status, &action, &item.Image, &item.ExpiresAt,
		&metadata, &item.ReadAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if item.Status == "delivered" {
		item.Status = "unread"
	}
	item.CategoryLabel = notificationdomain.CategoryLabel(item.Category)
	item.Action = decodeNotificationAction(action)
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

// buildAdminNotificationFilter 管理端筛选。full=true 连上用户、资料与回执（列表要展示这些列）；
// full=false 只连筛选用得到的表（计数、取 ID）。
func buildAdminNotificationFilter(appID int64, query notificationdomain.AdminExportQuery, full bool) (string, []any) {
	keyword := strings.TrimSpace(query.Keyword)
	category := strings.TrimSpace(query.Category)
	notificationType := strings.TrimSpace(query.Type)
	level := strings.TrimSpace(query.Level)
	status := strings.TrimSpace(query.Status)

	baseQuery := ` FROM notifications n`
	if full || keyword != "" {
		baseQuery += `
LEFT JOIN users u ON u.id = n.user_id
LEFT JOIN user_profiles p ON p.user_id = n.user_id`
	}
	if full || status == "read" || status == "unread" {
		baseQuery += `
LEFT JOIN notification_receipts nr ON nr.notification_id = n.id AND nr.user_id = n.user_id`
	}
	baseQuery += `
WHERE n.appid = $1`
	args := []any{appID}
	if category != "" && category != "all" {
		args = append(args, category)
		baseQuery += fmt.Sprintf(" AND n.category = $%d", len(args))
	}
	if notificationType != "" && notificationType != "all" {
		args = append(args, notificationType)
		baseQuery += fmt.Sprintf(" AND n.type = $%d", len(args))
	}
	if level != "" && level != "all" {
		args = append(args, level)
		baseQuery += fmt.Sprintf(" AND n.level = $%d", len(args))
	}
	switch status {
	case "read":
		baseQuery += " AND nr.status = 'read'"
	case "unread":
		baseQuery += " AND COALESCE(nr.status, n.status) <> 'read'"
	}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		baseQuery += fmt.Sprintf(" AND n.user_id = $%d", len(args))
	}
	if query.CampaignID > 0 {
		args = append(args, query.CampaignID)
		baseQuery += fmt.Sprintf(" AND n.campaign_id = $%d", len(args))
	}
	if keyword != "" {
		args = append(args, "%"+keyword+"%")
		baseQuery += fmt.Sprintf(`
  AND (
    COALESCE(n.title, '') ILIKE $%d
    OR COALESCE(n.content, '') ILIKE $%d
    OR COALESCE(u.account, '') ILIKE $%d
    OR COALESCE(p.nickname, '') ILIKE $%d
  )`, len(args), len(args), len(args), len(args))
	}
	return baseQuery, args
}
