package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	notifydomain "aegis/internal/domain/notify"

	"github.com/jackc/pgx/v5"
)

// 统一通知出口的数据访问层：渠道 / 订阅 / 模板 / 投递记录。
//
// secret_cipher 只在本层原样读写，加解密由 service 层完成（密钥派生自 SECURITY_MASTER_KEY），
// 保证 Repository 不持有任何密钥材料。

// ─────────────── 渠道 ───────────────

const notifyChannelColumns = `id, appid, key, name, kind, COALESCE(config, '{}'::jsonb), secret_cipher, secret_hint,
	enabled, rate_limit_per_minute, last_status, last_error, last_sent_at, created_by, created_at, updated_at`

func scanNotifyChannel(row interface{ Scan(dest ...any) error }) (*notifydomain.Channel, error) {
	item := &notifydomain.Channel{}
	var configRaw []byte
	var cipher string
	if err := row.Scan(&item.ID, &item.AppID, &item.Key, &item.Name, &item.Kind, &configRaw, &cipher, &item.SecretHint,
		&item.Enabled, &item.RateLimit, &item.LastStatus, &item.LastError, &item.LastSentAt,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.Config = map[string]any{}
	if len(configRaw) > 0 {
		_ = json.Unmarshal(configRaw, &item.Config)
	}
	// Secret 暂存密文，由 service 层解密后覆盖；SecretSet 供出网响应使用
	item.Secret = cipher
	item.SecretSet = strings.TrimSpace(cipher) != ""
	return item, nil
}

// ListNotifyChannels 渠道列表。appID<0 表示不限应用；appID>=0 时同时返回平台级（appid=0）。
func (r *Repository) ListNotifyChannels(ctx context.Context, appID int64, kind string, onlyEnabled bool) ([]notifydomain.Channel, error) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if appID >= 0 {
		args = append(args, appID)
		clauses = append(clauses, fmt.Sprintf("(appid = $%d OR appid = 0)", len(args)))
	}
	if strings.TrimSpace(kind) != "" {
		args = append(args, strings.TrimSpace(kind))
		clauses = append(clauses, fmt.Sprintf("kind = $%d", len(args)))
	}
	if onlyEnabled {
		clauses = append(clauses, "enabled = TRUE")
	}
	sql := "SELECT " + notifyChannelColumns + " FROM notify_channels"
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	sql += " ORDER BY appid ASC, id ASC"

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notifydomain.Channel, 0, 8)
	for rows.Next() {
		item, err := scanNotifyChannel(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetNotifyChannel 单条渠道。
func (r *Repository) GetNotifyChannel(ctx context.Context, id int64) (*notifydomain.Channel, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+notifyChannelColumns+" FROM notify_channels WHERE id = $1", id)
	item, err := scanNotifyChannel(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// GetNotifyChannelsByIDs 按 ID 批量取（定向投递用）。
func (r *Repository) GetNotifyChannelsByIDs(ctx context.Context, ids []int64) ([]notifydomain.Channel, error) {
	if len(ids) == 0 {
		return []notifydomain.Channel{}, nil
	}
	rows, err := r.pool.Query(ctx, "SELECT "+notifyChannelColumns+" FROM notify_channels WHERE id = ANY($1)", ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notifydomain.Channel, 0, len(ids))
	for rows.Next() {
		item, err := scanNotifyChannel(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// UpsertNotifyChannel 新建或更新渠道。cipher 为空字符串表示保持原值。
func (r *Repository) UpsertNotifyChannel(ctx context.Context, item notifydomain.Channel, cipher string, keepSecret bool) (*notifydomain.Channel, error) {
	configJSON, _ := json.Marshal(orEmptyMap(item.Config))
	if item.ID > 0 {
		secretSQL := "secret_cipher = $8, secret_hint = $9"
		if keepSecret {
			secretSQL = "secret_cipher = secret_cipher, secret_hint = secret_hint"
		}
		row := r.pool.QueryRow(ctx, `
			UPDATE notify_channels
			SET key = $2, name = $3, kind = $4, config = $5, enabled = $6, rate_limit_per_minute = $7,
			    `+secretSQL+`, updated_at = NOW()
			WHERE id = $1 RETURNING `+notifyChannelColumns,
			item.ID, item.Key, item.Name, item.Kind, configJSON, item.Enabled, item.RateLimit, cipher, item.SecretHint)
		return scanNotifyChannel(row)
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO notify_channels (appid, key, name, kind, config, secret_cipher, secret_hint, enabled, rate_limit_per_minute, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+notifyChannelColumns,
		item.AppID, item.Key, item.Name, item.Kind, configJSON, cipher, item.SecretHint, item.Enabled, item.RateLimit, item.CreatedBy)
	return scanNotifyChannel(row)
}

// DeleteNotifyChannel 删除渠道（级联删除其订阅）。
func (r *Repository) DeleteNotifyChannel(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM notify_channels WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// TouchNotifyChannelResult 记录渠道最近一次投递结果（自检状态灯）。
func (r *Repository) TouchNotifyChannelResult(ctx context.Context, id int64, status string, errMsg string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notify_channels SET last_status = $2, last_error = $3, last_sent_at = NOW() WHERE id = $1`,
		id, status, truncateSnippet(errMsg, 500))
	return err
}

// ─────────────── 订阅 ───────────────

const notifySubscriptionColumns = `s.id, s.channel_id, s.event_key, s.appid, COALESCE(s.min_priority, ''), s.category_ids,
	s.template_id, s.quiet_hours, s.enabled, s.created_at, s.updated_at, c.name, c.kind`

func scanNotifySubscription(row interface{ Scan(dest ...any) error }) (*notifydomain.Subscription, error) {
	item := &notifydomain.Subscription{}
	var quietRaw []byte
	var categoryIDs []int64
	if err := row.Scan(&item.ID, &item.ChannelID, &item.EventKey, &item.AppID, &item.MinPriority, &categoryIDs,
		&item.TemplateID, &quietRaw, &item.Enabled, &item.CreatedAt, &item.UpdatedAt,
		&item.ChannelName, &item.ChannelKind); err != nil {
		return nil, err
	}
	item.CategoryIDs = categoryIDs
	if len(quietRaw) > 0 {
		var quiet notifydomain.QuietHours
		if err := json.Unmarshal(quietRaw, &quiet); err == nil && strings.TrimSpace(quiet.Start) != "" {
			item.QuietHours = &quiet
		}
	}
	return item, nil
}

// ListNotifySubscriptions 全部订阅（管理端）。channelID>0 时按渠道过滤。
func (r *Repository) ListNotifySubscriptions(ctx context.Context, channelID int64) ([]notifydomain.Subscription, error) {
	sql := "SELECT " + notifySubscriptionColumns + " FROM notify_subscriptions s JOIN notify_channels c ON c.id = s.channel_id"
	args := make([]any, 0, 1)
	if channelID > 0 {
		args = append(args, channelID)
		sql += " WHERE s.channel_id = $1"
	}
	sql += " ORDER BY s.event_key ASC, s.id ASC"
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notifydomain.Subscription, 0, 16)
	for rows.Next() {
		item, err := scanNotifySubscription(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// MatchNotifySubscriptions 取出可能匹配该事件的启用订阅（含通配 `ticket.*` 与全局 `*`）。
// 精细过滤（优先级 / 分类 / 静默窗口）由 service 层完成。
func (r *Repository) MatchNotifySubscriptions(ctx context.Context, eventKey string, appID int64) ([]notifydomain.Subscription, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+notifySubscriptionColumns+`
		FROM notify_subscriptions s JOIN notify_channels c ON c.id = s.channel_id
		WHERE s.enabled = TRUE AND c.enabled = TRUE
		  AND s.event_key = ANY($1)
		  AND (s.appid IS NULL OR s.appid = $2)
		ORDER BY s.id ASC`, eventKeyMatchers(eventKey), appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notifydomain.Subscription, 0, 8)
	for rows.Next() {
		item, err := scanNotifySubscription(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// eventKeyMatchers 生成一个事件 key 能命中的全部订阅写法：
// 精确值 + 所有祖先通配 + 全局通配。
//
//	ticket.sla.warning → [ticket.sla.warning, ticket.sla.*, ticket.*, *]
//
// 早期实现只算最后一段（得到 ticket.sla.*），于是内置的 `ticket.*` 订阅
// 匹配不到任何 SLA 事件 —— SLA 预警/超时静默失踪。
func eventKeyMatchers(eventKey string) []string {
	eventKey = strings.TrimSpace(eventKey)
	matchers := []string{eventKey, "*"}
	for idx := strings.LastIndex(eventKey, "."); idx > 0; idx = strings.LastIndex(eventKey[:idx], ".") {
		matchers = append(matchers, eventKey[:idx]+".*")
	}
	return matchers
}

// UpsertNotifySubscription 新建或更新订阅。
func (r *Repository) UpsertNotifySubscription(ctx context.Context, item notifydomain.Subscription) (*notifydomain.Subscription, error) {
	var quietJSON []byte
	if item.QuietHours != nil {
		quietJSON, _ = json.Marshal(item.QuietHours)
	}
	categoryIDs := item.CategoryIDs
	if categoryIDs == nil {
		categoryIDs = []int64{}
	}
	minPriority := strings.TrimSpace(item.MinPriority)
	var id int64
	if item.ID > 0 {
		if err := r.pool.QueryRow(ctx, `
			UPDATE notify_subscriptions SET channel_id = $2, event_key = $3, appid = $4, min_priority = NULLIF($5,''),
			  category_ids = $6, template_id = $7, quiet_hours = $8, enabled = $9, updated_at = NOW()
			WHERE id = $1 RETURNING id`,
			item.ID, item.ChannelID, item.EventKey, item.AppID, minPriority, categoryIDs,
			item.TemplateID, quietJSON, item.Enabled).Scan(&id); err != nil {
			return nil, err
		}
	} else {
		if err := r.pool.QueryRow(ctx, `
			INSERT INTO notify_subscriptions (channel_id, event_key, appid, min_priority, category_ids, template_id, quiet_hours, enabled)
			VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8) RETURNING id`,
			item.ChannelID, item.EventKey, item.AppID, minPriority, categoryIDs,
			item.TemplateID, quietJSON, item.Enabled).Scan(&id); err != nil {
			return nil, err
		}
	}
	row := r.pool.QueryRow(ctx, "SELECT "+notifySubscriptionColumns+
		" FROM notify_subscriptions s JOIN notify_channels c ON c.id = s.channel_id WHERE s.id = $1", id)
	return scanNotifySubscription(row)
}

// DeleteNotifySubscription 删除订阅。
func (r *Repository) DeleteNotifySubscription(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM notify_subscriptions WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ─────────────── 模板 ───────────────

const notifyTemplateColumns = `id, appid, key, name, event_key, channel_kind, title_template, body_template,
	card_template, enabled, created_at, updated_at`

func scanNotifyTemplate(row interface{ Scan(dest ...any) error }) (*notifydomain.Template, error) {
	item := &notifydomain.Template{}
	var cardRaw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Key, &item.Name, &item.EventKey, &item.ChannelKind,
		&item.TitleTemplate, &item.BodyTemplate, &cardRaw, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if len(cardRaw) > 0 {
		_ = json.Unmarshal(cardRaw, &item.CardTemplate)
	}
	return item, nil
}

// ListNotifyTemplates 模板列表。
func (r *Repository) ListNotifyTemplates(ctx context.Context, appID int64) ([]notifydomain.Template, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+notifyTemplateColumns+
		" FROM notify_templates WHERE (appid = $1 OR appid = 0) ORDER BY appid ASC, event_key ASC, id ASC", appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notifydomain.Template, 0, 16)
	for rows.Next() {
		item, err := scanNotifyTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetNotifyTemplate 单条模板。
func (r *Repository) GetNotifyTemplate(ctx context.Context, id int64) (*notifydomain.Template, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+notifyTemplateColumns+" FROM notify_templates WHERE id = $1", id)
	item, err := scanNotifyTemplate(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// ResolveNotifyTemplate 按 事件 key + 渠道类型 找默认模板：
// 优先「应用级 + 精确渠道类型」，其次「应用级 + 通用」，再退到平台级。
func (r *Repository) ResolveNotifyTemplate(ctx context.Context, eventKey string, channelKind string, appID int64) (*notifydomain.Template, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+notifyTemplateColumns+`
		FROM notify_templates
		WHERE enabled = TRUE AND event_key = $1 AND (appid = $2 OR appid = 0)
		  AND (channel_kind = $3 OR channel_kind = '')
		ORDER BY (appid = $2) DESC, (channel_kind = $3) DESC, id ASC
		LIMIT 1`, eventKey, appID, channelKind)
	item, err := scanNotifyTemplate(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// UpsertNotifyTemplate 新建或更新模板。
func (r *Repository) UpsertNotifyTemplate(ctx context.Context, item notifydomain.Template) (*notifydomain.Template, error) {
	var cardJSON []byte
	if item.CardTemplate != nil {
		cardJSON, _ = json.Marshal(item.CardTemplate)
	}
	if item.ID > 0 {
		row := r.pool.QueryRow(ctx, `
			UPDATE notify_templates SET key = $2, name = $3, event_key = $4, channel_kind = $5,
			  title_template = $6, body_template = $7, card_template = $8, enabled = $9, updated_at = NOW()
			WHERE id = $1 RETURNING `+notifyTemplateColumns,
			item.ID, item.Key, item.Name, item.EventKey, item.ChannelKind,
			item.TitleTemplate, item.BodyTemplate, cardJSON, item.Enabled)
		return scanNotifyTemplate(row)
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO notify_templates (appid, key, name, event_key, channel_kind, title_template, body_template, card_template, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+notifyTemplateColumns,
		item.AppID, item.Key, item.Name, item.EventKey, item.ChannelKind,
		item.TitleTemplate, item.BodyTemplate, cardJSON, item.Enabled)
	return scanNotifyTemplate(row)
}

// DeleteNotifyTemplate 删除模板。
func (r *Repository) DeleteNotifyTemplate(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM notify_templates WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ─────────────── 投递记录 ───────────────

// InsertNotifyDelivery 写入一条投递记录，返回 ID。
// 命中 dedupe_key 唯一索引时返回 (0, nil)，调用方据此跳过重复投递。
func (r *Repository) InsertNotifyDelivery(ctx context.Context, item notifydomain.Delivery) (int64, error) {
	var dedupe *string
	if key := strings.TrimSpace(item.DedupeKey); key != "" {
		dedupe = &key
	}
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO notify_deliveries (channel_id, channel_kind, event_key, appid, resource, resource_id, dedupe_key, status, attempt)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		RETURNING id`,
		item.ChannelID, item.ChannelKind, item.EventKey, item.AppID,
		item.Resource, item.ResourceID, dedupe, notifydomain.DeliveryPending, 0).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// CompleteNotifyDelivery 回填投递结果。
func (r *Repository) CompleteNotifyDelivery(ctx context.Context, id int64, status string, attempt int,
	requestSnippet, responseSnippet, errMsg string, latencyMs int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE notify_deliveries
		SET status = $2, attempt = $3, request_snippet = $4, response_snippet = $5, error = $6,
		    latency_ms = $7, completed_at = NOW()
		WHERE id = $1`,
		id, status, attempt,
		truncateSnippet(requestSnippet, 2000), truncateSnippet(responseSnippet, 2000),
		truncateSnippet(errMsg, 1000), latencyMs)
	return err
}

// ListNotifyDeliveries 投递记录分页。
func (r *Repository) ListNotifyDeliveries(ctx context.Context, query notifydomain.DeliveryQuery) ([]notifydomain.Delivery, int64, error) {
	clauses := make([]string, 0, 6)
	args := make([]any, 0, 8)
	if query.ChannelID != nil {
		args = append(args, *query.ChannelID)
		clauses = append(clauses, fmt.Sprintf("d.channel_id = $%d", len(args)))
	}
	if strings.TrimSpace(query.EventKey) != "" {
		args = append(args, strings.TrimSpace(query.EventKey))
		clauses = append(clauses, fmt.Sprintf("d.event_key = $%d", len(args)))
	}
	if strings.TrimSpace(query.Status) != "" {
		args = append(args, strings.TrimSpace(query.Status))
		clauses = append(clauses, fmt.Sprintf("d.status = $%d", len(args)))
	}
	if strings.TrimSpace(query.Resource) != "" {
		args = append(args, strings.TrimSpace(query.Resource))
		clauses = append(clauses, fmt.Sprintf("d.resource = $%d", len(args)))
	}
	if strings.TrimSpace(query.ResourceID) != "" {
		args = append(args, strings.TrimSpace(query.ResourceID))
		clauses = append(clauses, fmt.Sprintf("d.resource_id = $%d", len(args)))
	}
	if query.AppID != nil {
		args = append(args, *query.AppID)
		clauses = append(clauses, fmt.Sprintf("d.appid = $%d", len(args)))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM notify_deliveries d"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []notifydomain.Delivery{}, 0, nil
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
	sql := `SELECT d.id, d.channel_id, COALESCE(c.name, ''), d.channel_kind, d.event_key, d.appid,
		d.resource, d.resource_id, COALESCE(d.dedupe_key, ''), d.status, d.attempt,
		d.request_snippet, d.response_snippet, d.error, d.latency_ms, d.created_at, d.completed_at
		FROM notify_deliveries d LEFT JOIN notify_channels c ON c.id = d.channel_id` + where +
		fmt.Sprintf(" ORDER BY d.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]notifydomain.Delivery, 0, limit)
	for rows.Next() {
		var item notifydomain.Delivery
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.ChannelName, &item.ChannelKind, &item.EventKey, &item.AppID,
			&item.Resource, &item.ResourceID, &item.DedupeKey, &item.Status, &item.Attempt,
			&item.RequestSnippet, &item.ResponseSnippet, &item.Error, &item.LatencyMs,
			&item.CreatedAt, &item.CompletedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// NotifyDeliveryStats 最近 N 天投递健康度。
func (r *Repository) NotifyDeliveryStats(ctx context.Context, days int) (*notifydomain.DeliveryStats, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	stats := &notifydomain.DeliveryStats{ByKind: map[string]int64{}, ByEvent: map[string]int64{}}
	sql := fmt.Sprintf(`SELECT COUNT(*),
		COUNT(*) FILTER (WHERE status = 'success'),
		COUNT(*) FILTER (WHERE status = 'failed'),
		COUNT(*) FILTER (WHERE status = 'skipped'),
		COALESCE(AVG(latency_ms) FILTER (WHERE status = 'success'), 0)
		FROM notify_deliveries WHERE created_at >= NOW() - INTERVAL '%d days'`, days)
	var avgLatency float64
	if err := r.pool.QueryRow(ctx, sql).Scan(&stats.Total, &stats.Success, &stats.Failed, &stats.Skipped, &avgLatency); err != nil {
		return nil, err
	}
	stats.AvgLatency = int64(avgLatency)
	if stats.Total > 0 {
		stats.SuccessPct = float64(stats.Success) / float64(stats.Total) * 100
	}

	kindSQL := fmt.Sprintf(`SELECT channel_kind, COUNT(*) FROM notify_deliveries
		WHERE created_at >= NOW() - INTERVAL '%d days' GROUP BY channel_kind`, days)
	rows, err := r.pool.Query(ctx, kindSQL)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind string
		var count int64
		if err := rows.Scan(&kind, &count); err != nil {
			rows.Close()
			return nil, err
		}
		stats.ByKind[kind] = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	eventSQL := fmt.Sprintf(`SELECT event_key, COUNT(*) FROM notify_deliveries
		WHERE created_at >= NOW() - INTERVAL '%d days' GROUP BY event_key ORDER BY COUNT(*) DESC LIMIT 20`, days)
	eventRows, err := r.pool.Query(ctx, eventSQL)
	if err != nil {
		return nil, err
	}
	defer eventRows.Close()
	for eventRows.Next() {
		var key string
		var count int64
		if err := eventRows.Scan(&key, &count); err != nil {
			return nil, err
		}
		stats.ByEvent[key] = count
	}
	return stats, eventRows.Err()
}

// PurgeNotifyDeliveries 清理 N 天前的投递记录，返回删除条数。
func (r *Repository) PurgeNotifyDeliveries(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		days = 30
	}
	tag, err := r.pool.Exec(ctx, fmt.Sprintf(`DELETE FROM notify_deliveries WHERE created_at < NOW() - INTERVAL '%d days'`, days))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// AdminContact 通知投递需要的最小管理员信息。
type AdminContact struct {
	ID          int64
	Account     string
	DisplayName string
	Email       string
}

// ListAdminContactsByIDs 批量取管理员联系方式（邮件 / 站内提示的收件目标）。
func (r *Repository) ListAdminContactsByIDs(ctx context.Context, ids []int64) ([]AdminContact, error) {
	if len(ids) == 0 {
		return []AdminContact{}, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, account, COALESCE(display_name, ''), COALESCE(email, '')
		 FROM admin_accounts WHERE id = ANY($1) AND status = 'active'`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AdminContact, 0, len(ids))
	for rows.Next() {
		var item AdminContact
		if err := rows.Scan(&item.ID, &item.Account, &item.DisplayName, &item.Email); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func truncateSnippet(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max] + "…"
}
