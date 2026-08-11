package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	emaildomain "aegis/internal/domain/email"
)

const emailConfigColumns = `id, appid, name, provider, enabled, is_default, COALESCE(description, ''), COALESCE(config, '{}'::jsonb), created_at, updated_at`

// emailConfigPayload 是 app_email_configs.config 这一列的落库形态。
//
// SMTPConfig 用内嵌（而非嵌套字段）是刻意的：历史数据里该列就是**扁平的 SMTP 字段**，
// 内嵌能让旧行原样解出来，新增的 zeabur 只是多一个同级键。改成嵌套会让存量配置全部失效。
type emailConfigPayload struct {
	emaildomain.SMTPConfig
	Zeabur *zeaburConfigPayload `json:"zeabur,omitempty"`
}

// zeaburConfigPayload 只落密文字段，明文 APIKey / WebhookSecret 永远不进数据库。
type zeaburConfigPayload struct {
	APIKeyCipher        string            `json:"apiKeyCipher,omitempty"`
	BaseURL             string            `json:"baseUrl,omitempty"`
	FromAddress         string            `json:"fromAddress,omitempty"`
	FromName            string            `json:"fromName,omitempty"`
	ReplyTo             string            `json:"replyTo,omitempty"`
	WebhookSecretCipher string            `json:"webhookSecretCipher,omitempty"`
	Tags                map[string]string `json:"tags,omitempty"`
}

func (r *Repository) ListEmailConfigs(ctx context.Context, appID int64) ([]emaildomain.Config, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+emailConfigColumns+` FROM app_email_configs WHERE appid = $1 ORDER BY is_default DESC, id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]emaildomain.Config, 0, 4)
	for rows.Next() {
		item, err := scanEmailConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetEmailConfigByID(ctx context.Context, appID int64, id int64) (*emaildomain.Config, error) {
	return scanEmailConfig(r.pool.QueryRow(ctx, `SELECT `+emailConfigColumns+` FROM app_email_configs WHERE appid = $1 AND id = $2 LIMIT 1`, appID, id))
}

func (r *Repository) GetEmailConfigByName(ctx context.Context, appID int64, name string) (*emaildomain.Config, error) {
	return scanEmailConfig(r.pool.QueryRow(ctx, `SELECT `+emailConfigColumns+` FROM app_email_configs WHERE appid = $1 AND name = $2 LIMIT 1`, appID, name))
}

func (r *Repository) GetDefaultEmailConfig(ctx context.Context, appID int64) (*emaildomain.Config, error) {
	return scanEmailConfig(r.pool.QueryRow(ctx, `SELECT `+emailConfigColumns+` FROM app_email_configs WHERE appid = $1 AND enabled = TRUE ORDER BY is_default DESC, id ASC LIMIT 1`, appID))
}

func (r *Repository) UpsertEmailConfig(ctx context.Context, item emaildomain.Config) (*emaildomain.Config, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if item.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE app_email_configs SET is_default = FALSE, updated_at = NOW() WHERE appid = $1 AND id <> $2`, item.AppID, item.ID); err != nil {
			return nil, err
		}
	}

	configJSON, err := marshalEmailConfigPayload(item)
	if err != nil {
		return nil, err
	}
	var (
		query string
		saved *emaildomain.Config
	)
	if item.ID > 0 {
		query = `INSERT INTO app_email_configs (id, appid, name, provider, enabled, is_default, description, config, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
	name = EXCLUDED.name,
	provider = EXCLUDED.provider,
	enabled = EXCLUDED.enabled,
	is_default = EXCLUDED.is_default,
	description = EXCLUDED.description,
	config = EXCLUDED.config,
	updated_at = NOW()
RETURNING ` + emailConfigColumns
		saved, err = scanEmailConfig(tx.QueryRow(ctx, query, item.ID, item.AppID, item.Name, item.Provider, item.Enabled, item.IsDefault, nullableString(item.Description), configJSON))
	} else {
		query = `INSERT INTO app_email_configs (appid, name, provider, enabled, is_default, description, config, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
RETURNING ` + emailConfigColumns
		saved, err = scanEmailConfig(tx.QueryRow(ctx, query, item.AppID, item.Name, item.Provider, item.Enabled, item.IsDefault, nullableString(item.Description), configJSON))
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

func (r *Repository) DeleteEmailConfig(ctx context.Context, appID int64, id int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM app_email_configs WHERE appid = $1 AND id = $2`, appID, id)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func marshalEmailConfigPayload(item emaildomain.Config) ([]byte, error) {
	payload := emailConfigPayload{SMTPConfig: item.SMTP}
	zeabur := item.Zeabur
	if strings.TrimSpace(zeabur.APIKeyCipher) != "" ||
		strings.TrimSpace(zeabur.BaseURL) != "" ||
		strings.TrimSpace(zeabur.FromAddress) != "" ||
		strings.TrimSpace(zeabur.FromName) != "" ||
		strings.TrimSpace(zeabur.ReplyTo) != "" ||
		strings.TrimSpace(zeabur.WebhookSecretCipher) != "" ||
		len(zeabur.Tags) > 0 {
		payload.Zeabur = &zeaburConfigPayload{
			APIKeyCipher:        zeabur.APIKeyCipher,
			BaseURL:             strings.TrimSpace(zeabur.BaseURL),
			FromAddress:         strings.TrimSpace(zeabur.FromAddress),
			FromName:            strings.TrimSpace(zeabur.FromName),
			ReplyTo:             strings.TrimSpace(zeabur.ReplyTo),
			WebhookSecretCipher: zeabur.WebhookSecretCipher,
			Tags:                zeabur.Tags,
		}
	}
	return json.Marshal(payload)
}

func scanEmailConfig(row interface{ Scan(dest ...any) error }) (*emaildomain.Config, error) {
	var item emaildomain.Config
	var configBytes []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Provider, &item.Enabled, &item.IsDefault, &item.Description, &configBytes, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	var payload emailConfigPayload
	_ = json.Unmarshal(configBytes, &payload)
	item.SMTP = payload.SMTPConfig
	if payload.Zeabur != nil {
		item.Zeabur = emaildomain.ZeaburConfig{
			APIKeyCipher:        payload.Zeabur.APIKeyCipher,
			APIKeySet:           strings.TrimSpace(payload.Zeabur.APIKeyCipher) != "",
			BaseURL:             payload.Zeabur.BaseURL,
			FromAddress:         payload.Zeabur.FromAddress,
			FromName:            payload.Zeabur.FromName,
			ReplyTo:             payload.Zeabur.ReplyTo,
			WebhookSecretCipher: payload.Zeabur.WebhookSecretCipher,
			WebhookSecretSet:    strings.TrimSpace(payload.Zeabur.WebhookSecretCipher) != "",
			Tags:                payload.Zeabur.Tags,
		}
	}
	return &item, nil
}

// ── 投递记录 ──

// CreateEmailDelivery 落一条投递留痕，返回带自增 ID 的记录。
func (r *Repository) CreateEmailDelivery(ctx context.Context, item emaildomain.Delivery) (*emaildomain.Delivery, error) {
	payload, err := json.Marshal(orEmptyMap(item.LastEventPayload))
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx, `INSERT INTO email_deliveries
	(appid, config_id, config_name, provider, provider_message_id, message_id, to_address, from_address, subject, purpose, status, error_message, open_count, click_count, last_event, last_event_payload, sent_at, delivered_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 0, 0, $13, $14, $15, $16, NOW(), NOW())
RETURNING `+emailDeliveryColumns,
		item.AppID, nullableInt64(item.ConfigID), nullableString(item.ConfigName), item.Provider,
		nullableString(item.ProviderMessageID), nullableString(item.MessageID),
		item.ToAddress, nullableString(item.FromAddress), nullableString(item.Subject), nullableString(item.Purpose),
		item.Status, nullableString(item.ErrorMessage), nullableString(item.LastEvent), payload,
		nullableTimePtr(item.SentAt), nullableTimePtr(item.DeliveredAt))
	return scanEmailDelivery(row)
}

// EmailDeliveryStatusUpdate 描述一次 webhook 事件对投递记录的增量修改。
type EmailDeliveryStatusUpdate struct {
	// AppID 收窄更新范围：webhook 已按应用验签，带上它能保证 A 应用的密钥
	// 无论如何都改不到 B 应用的记录。
	AppID             int64
	ProviderMessageID string
	Status            string
	Event             string
	ErrorMessage      string
	Payload           map[string]any
	OccurredAt        time.Time
	IncrementOpen     bool
	IncrementClick    bool
	MarkDelivered     bool
}

// ApplyEmailDeliveryEvent 按 provider_message_id 回填状态。
//
// 状态只允许**单向推进**：终态（bounced/complained/rejected/failed）不会被随后的
// delivery 事件覆盖回去，open/click 也不改主状态 —— 否则乱序到达的 webhook
// 会把一封已退信的邮件显示成投递成功。返回 false 表示没有匹配到记录。
func (r *Repository) ApplyEmailDeliveryEvent(ctx context.Context, update EmailDeliveryStatusUpdate) (bool, error) {
	providerMessageID := strings.TrimSpace(update.ProviderMessageID)
	if providerMessageID == "" {
		return false, nil
	}
	payload, err := json.Marshal(orEmptyMap(update.Payload))
	if err != nil {
		return false, err
	}
	result, err := r.pool.Exec(ctx, `UPDATE email_deliveries SET
	status = CASE
		WHEN $2 = '' THEN status
		WHEN status IN ('bounced', 'complained', 'rejected', 'failed') THEN status
		WHEN status = 'delivered' AND $2 = 'sent' THEN status
		ELSE $2
	END,
	last_event = $3,
	last_event_payload = $4,
	error_message = CASE WHEN $5 = '' THEN error_message ELSE $5 END,
	open_count = open_count + CASE WHEN $6 THEN 1 ELSE 0 END,
	click_count = click_count + CASE WHEN $7 THEN 1 ELSE 0 END,
	delivered_at = CASE WHEN $8 AND delivered_at IS NULL THEN COALESCE($9, NOW()) ELSE delivered_at END,
	updated_at = NOW()
WHERE provider_message_id = $1 AND appid = $10`,
		providerMessageID, update.Status, update.Event, payload, update.ErrorMessage,
		update.IncrementOpen, update.IncrementClick, update.MarkDelivered, nullableTime(update.OccurredAt), update.AppID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

// CountEmailDeliveriesSince 统计某个用途、某个收件地址在给定时刻之后的发信次数。
//
// 用于凭证补发这类「用户可以自助触发发信」的接口做频次限制。
// 之所以不另建计数表：投递留痕本来就逐封记录，再存一份计数只会多一处可能对不上的事实。
// 失败的投递也计入 —— 反复触发失败发信同样在消耗上游配额。
func (r *Repository) CountEmailDeliveriesSince(ctx context.Context, appID int64, purpose string, toAddress string, since time.Time) (int64, error) {
	var total int64
	query := `SELECT COUNT(*) FROM email_deliveries
WHERE appid = $1 AND purpose = $2 AND lower(to_address) = lower($3) AND created_at >= $4`
	if err := r.pool.QueryRow(ctx, query, appID, purpose, strings.TrimSpace(toAddress), since).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *Repository) ListEmailDeliveries(ctx context.Context, query emaildomain.DeliveryQuery) (*emaildomain.DeliveryPage, error) {
	page := query.Page
	if page <= 0 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	conditions := []string{"appid = $1"}
	args := []any{query.AppID}
	if query.ConfigID > 0 {
		args = append(args, query.ConfigID)
		conditions = append(conditions, fmt.Sprintf("config_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if provider := strings.TrimSpace(query.Provider); provider != "" {
		args = append(args, provider)
		conditions = append(conditions, fmt.Sprintf("provider = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		conditions = append(conditions, fmt.Sprintf("(to_address ILIKE $%d OR subject ILIKE $%d)", len(args), len(args)))
	}
	where := "WHERE " + strings.Join(conditions, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM email_deliveries `+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := r.pool.Query(ctx, `SELECT `+emailDeliveryColumns+` FROM email_deliveries `+where+
		fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]emaildomain.Delivery, 0, pageSize)
	for rows.Next() {
		item, err := scanEmailDelivery(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &emaildomain.DeliveryPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

const emailDeliveryColumns = `id, appid, COALESCE(config_id, 0), COALESCE(config_name, ''), provider, COALESCE(provider_message_id, ''), COALESCE(message_id, ''), to_address, COALESCE(from_address, ''), COALESCE(subject, ''), COALESCE(purpose, ''), status, COALESCE(error_message, ''), open_count, click_count, COALESCE(last_event, ''), COALESCE(last_event_payload, '{}'::jsonb), sent_at, delivered_at, created_at, updated_at`

func scanEmailDelivery(row interface{ Scan(dest ...any) error }) (*emaildomain.Delivery, error) {
	var item emaildomain.Delivery
	var payloadBytes []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.ConfigID, &item.ConfigName, &item.Provider,
		&item.ProviderMessageID, &item.MessageID, &item.ToAddress, &item.FromAddress, &item.Subject,
		&item.Purpose, &item.Status, &item.ErrorMessage, &item.OpenCount, &item.ClickCount,
		&item.LastEvent, &payloadBytes, &item.SentAt, &item.DeliveredAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(payloadBytes) > 0 {
		_ = json.Unmarshal(payloadBytes, &item.LastEventPayload)
	}
	return &item, nil
}
