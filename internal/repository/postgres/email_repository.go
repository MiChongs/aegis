package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	emaildomain "aegis/internal/domain/email"
)

// appid 在库里用 NULL 表示平台级，Go 侧用 0（emaildomain.PlatformAppID）。
// 读取一律 COALESCE 回 0，写入一律经 nullableAppID —— 这个映射只在本文件出现，
// 上层看到的永远是一个 int64。
const emailConfigColumns = `id, COALESCE(appid, 0), name, provider, enabled, is_default, shared, COALESCE(description, ''), COALESCE(config, '{}'::jsonb), created_at, updated_at`

// nullableAppID 把 Go 侧的 0 翻成库里的 NULL。
func nullableAppID(appID int64) any {
	if appID == emaildomain.PlatformAppID {
		return nil
	}
	return appID
}

// emailScopeCondition 生成作用域过滤片段。
//
// 刻意不写成 `COALESCE(appid, 0) = $1` —— 那个表达式让 appid 上的所有索引失效，
// 而投递记录表会长到几百万行。分成 IS NULL 与等值两条，两边都走得上索引。
func emailScopeCondition(appID int64, args *[]any) string {
	if appID == emaildomain.PlatformAppID {
		return "appid IS NULL"
	}
	*args = append(*args, appID)
	return fmt.Sprintf("appid = $%d", len(*args))
}

// emailConfigPayload 是 app_email_configs.config 这一列的落库形态。
//
// 写入只产出 options / secrets 两个通用袋子（键的含义由服务商目录声明）；
// 上面那批扁平字段与 zeabur 子对象**只读不写**，用来解出重构之前落库的行。
// 存量行因此零迁移：下一次保存时自动变成新形态，在那之前照常可用。
type emailConfigPayload struct {
	// ── 通用形态（当前写入的唯一形态）──
	Options map[string]string `json:"options,omitempty"`
	// Secrets 是密文，明文密钥永远不进数据库。
	Secrets map[string]string `json:"secrets,omitempty"`

	// ── 存量形态（只读）──
	LegacyHost               string               `json:"host,omitempty"`
	LegacyPort               int                  `json:"port,omitempty"`
	LegacyUsername           string               `json:"username,omitempty"`
	LegacyPassword           string               `json:"password,omitempty"`
	LegacyFromAddress        string               `json:"fromAddress,omitempty"`
	LegacyFromName           string               `json:"fromName,omitempty"`
	LegacyReplyTo            string               `json:"replyTo,omitempty"`
	LegacyUseTLS             bool                 `json:"useTLS,omitempty"`
	LegacyInsecureSkipVerify bool                 `json:"insecureSkipVerify,omitempty"`
	LegacyMaxConnections     int                  `json:"maxConnections,omitempty"`
	LegacyMaxMessagesPerConn int                  `json:"maxMessagesPerConn,omitempty"`
	LegacyZeabur             *legacyZeaburPayload `json:"zeabur,omitempty"`
}

type legacyZeaburPayload struct {
	APIKeyCipher        string            `json:"apiKeyCipher,omitempty"`
	BaseURL             string            `json:"baseUrl,omitempty"`
	FromAddress         string            `json:"fromAddress,omitempty"`
	FromName            string            `json:"fromName,omitempty"`
	ReplyTo             string            `json:"replyTo,omitempty"`
	WebhookSecretCipher string            `json:"webhookSecretCipher,omitempty"`
	Tags                map[string]string `json:"tags,omitempty"`
}

func (r *Repository) ListEmailConfigs(ctx context.Context, appID int64) ([]emaildomain.Config, error) {
	args := make([]any, 0, 1)
	where := emailScopeCondition(appID, &args)
	rows, err := r.pool.Query(ctx, `SELECT `+emailConfigColumns+` FROM app_email_configs WHERE `+where+` ORDER BY is_default DESC, id ASC`, args...)
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
	args := make([]any, 0, 2)
	where := emailScopeCondition(appID, &args)
	args = append(args, id)
	return scanEmailConfig(r.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM app_email_configs WHERE %s AND id = $%d LIMIT 1`, emailConfigColumns, where, len(args)), args...))
}

func (r *Repository) GetEmailConfigByName(ctx context.Context, appID int64, name string) (*emaildomain.Config, error) {
	args := make([]any, 0, 2)
	where := emailScopeCondition(appID, &args)
	args = append(args, name)
	return scanEmailConfig(r.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM app_email_configs WHERE %s AND name = $%d LIMIT 1`, emailConfigColumns, where, len(args)), args...))
}

func (r *Repository) GetDefaultEmailConfig(ctx context.Context, appID int64) (*emaildomain.Config, error) {
	args := make([]any, 0, 1)
	where := emailScopeCondition(appID, &args)
	return scanEmailConfig(r.pool.QueryRow(ctx,
		`SELECT `+emailConfigColumns+` FROM app_email_configs WHERE `+where+` AND enabled = TRUE ORDER BY is_default DESC, id ASC LIMIT 1`, args...))
}

// GetSharedPlatformEmailConfig 取平台级**已共享**的默认通道。
//
// 这是应用没有任何自有配置时的回落目标。查询里同时钉住 shared 与 enabled：
// 平台管理员关掉共享开关的那一刻，回落必须立即停止 ——
// 把判定放到 Go 里做会让「已经取到配置了再检查」变成一次可省略的步骤，
// 而可省略的检查迟早会被某条新链路省掉。
func (r *Repository) GetSharedPlatformEmailConfig(ctx context.Context) (*emaildomain.Config, error) {
	return scanEmailConfig(r.pool.QueryRow(ctx,
		`SELECT `+emailConfigColumns+` FROM app_email_configs
WHERE appid IS NULL AND shared = TRUE AND enabled = TRUE
ORDER BY is_default DESC, id ASC LIMIT 1`))
}

func (r *Repository) UpsertEmailConfig(ctx context.Context, item emaildomain.Config) (*emaildomain.Config, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	scopeArgs := make([]any, 0, 2)
	scopeWhere := emailScopeCondition(item.AppID, &scopeArgs)
	if item.IsDefault {
		scopeArgs = append(scopeArgs, item.ID)
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`UPDATE app_email_configs SET is_default = FALSE, updated_at = NOW() WHERE %s AND id <> $%d`, scopeWhere, len(scopeArgs)),
			scopeArgs...); err != nil {
			return nil, err
		}
	}

	configJSON, err := marshalEmailConfigPayload(item)
	if err != nil {
		return nil, err
	}
	// shared 只在平台级配置上有意义，落库前夹一遍。
	// 库里那条 CHECK 只是最后一道保险 —— 撞上它的表现是一句 SQL 报错，
	// 而管理员看到的应该是「这一项对应用级配置无效」而不是约束名。
	shared := item.Shared && item.IsPlatform()

	var saved *emaildomain.Config
	if item.ID > 0 {
		saved, err = scanEmailConfig(tx.QueryRow(ctx, `INSERT INTO app_email_configs (id, appid, name, provider, enabled, is_default, shared, description, config, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
	name = EXCLUDED.name,
	provider = EXCLUDED.provider,
	enabled = EXCLUDED.enabled,
	is_default = EXCLUDED.is_default,
	shared = EXCLUDED.shared,
	description = EXCLUDED.description,
	config = EXCLUDED.config,
	updated_at = NOW()
RETURNING `+emailConfigColumns,
			item.ID, nullableAppID(item.AppID), item.Name, item.Provider, item.Enabled, item.IsDefault, shared, nullableString(item.Description), configJSON))
	} else {
		saved, err = scanEmailConfig(tx.QueryRow(ctx, `INSERT INTO app_email_configs (appid, name, provider, enabled, is_default, shared, description, config, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
RETURNING `+emailConfigColumns,
			nullableAppID(item.AppID), item.Name, item.Provider, item.Enabled, item.IsDefault, shared, nullableString(item.Description), configJSON))
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
	args := make([]any, 0, 2)
	where := emailScopeCondition(appID, &args)
	args = append(args, id)
	result, err := r.pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM app_email_configs WHERE %s AND id = $%d`, where, len(args)), args...)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

// CountEmailConfigs 某个作用域下有几条配置。
// 删除最后一条平台通道之前要能提示影响面，因此单独给一个计数。
func (r *Repository) CountEmailConfigs(ctx context.Context, appID int64) (int64, error) {
	args := make([]any, 0, 1)
	where := emailScopeCondition(appID, &args)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_email_configs WHERE `+where, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func marshalEmailConfigPayload(item emaildomain.Config) ([]byte, error) {
	payload := emailConfigPayload{
		Options: compactStringMap(item.Settings),
		Secrets: compactStringMap(item.SecretsCipher),
	}
	return json.Marshal(payload)
}

// compactStringMap 丢掉空值键：留着它们只会让 config 这一列随着服务商切换
// 不断堆积上一家的空字段，而 JSON 里一个空串与「没有这个键」在读取端是同一件事。
func compactStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		key = strings.TrimSpace(key)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func scanEmailConfig(row interface{ Scan(dest ...any) error }) (*emaildomain.Config, error) {
	var item emaildomain.Config
	var configBytes []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Provider, &item.Enabled, &item.IsDefault,
		&item.Shared, &item.Description, &configBytes, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	var payload emailConfigPayload
	_ = json.Unmarshal(configBytes, &payload)

	item.Settings = map[string]string{}
	item.Secrets = map[string]string{}
	item.SecretsCipher = map[string]string{}

	if len(payload.Options) > 0 || len(payload.Secrets) > 0 {
		for key, value := range payload.Options {
			item.Settings[key] = value
		}
		for key, value := range payload.Secrets {
			item.SecretsCipher[key] = value
		}
	} else {
		hydrateLegacyEmailPayload(&item, payload)
	}
	return &item, nil
}

// hydrateLegacyEmailPayload 把重构之前落库的行解成通用形态。
//
// 按 provider 分流是必须的：旧代码无论服务商是什么都会把 SMTP 段原样写进去
// （那一段是内嵌的），因此一条 zeabur 配置的 JSON 里同样有 host / port /
// fromAddress —— 不分流的话，Zeabur 配置的发件地址会被 SMTP 段那个覆盖掉。
func hydrateLegacyEmailPayload(item *emaildomain.Config, payload emailConfigPayload) {
	set := func(key string, value string) {
		if strings.TrimSpace(value) != "" {
			item.Settings[key] = value
		}
	}

	if strings.EqualFold(strings.TrimSpace(item.Provider), emaildomain.ProviderZeabur) {
		legacy := payload.LegacyZeabur
		if legacy == nil {
			return
		}
		set(emaildomain.KeyFromAddress, legacy.FromAddress)
		set(emaildomain.KeyFromName, legacy.FromName)
		set(emaildomain.KeyReplyTo, legacy.ReplyTo)
		set("baseUrl", legacy.BaseURL)
		if len(legacy.Tags) > 0 {
			if encoded, err := json.Marshal(legacy.Tags); err == nil {
				set(emaildomain.KeyTags, string(encoded))
			}
		}
		if cipher := strings.TrimSpace(legacy.APIKeyCipher); cipher != "" {
			item.SecretsCipher["apiKey"] = cipher
		}
		if cipher := strings.TrimSpace(legacy.WebhookSecretCipher); cipher != "" {
			item.SecretsCipher[emaildomain.KeyWebhookSecret] = cipher
		}
		return
	}

	// 其余一律按 SMTP 解：重构前只有 smtp 与 zeabur 两档。
	set("host", payload.LegacyHost)
	if payload.LegacyPort > 0 {
		set("port", strconv.Itoa(payload.LegacyPort))
	}
	set("username", payload.LegacyUsername)
	set(emaildomain.KeyFromAddress, payload.LegacyFromAddress)
	set(emaildomain.KeyFromName, payload.LegacyFromName)
	set(emaildomain.KeyReplyTo, payload.LegacyReplyTo)
	// 旧的 useTLS 布尔位翻成 encryption 枚举：true = 465 的隐式 TLS，
	// false = 旧代码里那条 TLSMandatory 分支，也就是 STARTTLS。
	// 不在这里翻的话，存量的 465 配置会在控制台上显示成 STARTTLS，
	// 而管理员保存一次就真的变成 STARTTLS 了 —— 那是一次静默的配置损坏。
	if payload.LegacyUseTLS {
		set("encryption", emaildomain.SMTPEncryptionSSL)
	} else if payload.LegacyHost != "" {
		set("encryption", emaildomain.SMTPEncryptionSTARTTLS)
	}
	if payload.LegacyInsecureSkipVerify {
		set("insecureSkipVerify", "true")
	}
	// SMTP 密码此前是**明文**落库的。这里如实解出来放进明文袋子，
	// 服务层在下一次保存时会把它加密写回（自愈），因此不需要迁移脚本。
	// 不这么做的话，改一次发件人名就会把这条配置的密码清空。
	if password := strings.TrimSpace(payload.LegacyPassword); password != "" {
		item.Secrets["password"] = password
	}
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
		nullableAppID(item.AppID), nullableInt64(item.ConfigID), nullableString(item.ConfigName), item.Provider,
		nullableString(item.ProviderMessageID), nullableString(item.MessageID),
		item.ToAddress, nullableString(item.FromAddress), nullableString(item.Subject), nullableString(item.Purpose),
		item.Status, nullableString(item.ErrorMessage), nullableString(item.LastEvent), payload,
		nullableTimePtr(item.SentAt), nullableTimePtr(item.DeliveredAt))
	return scanEmailDelivery(row)
}

// EmailDeliveryStatusUpdate 描述一次 webhook 事件对投递记录的增量修改。
type EmailDeliveryStatusUpdate struct {
	// AppID 收窄更新范围：webhook 已按作用域验签，带上它能保证 A 应用的密钥
	// 无论如何都改不到 B 应用的记录（0 即平台级）。
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
	args := []any{
		providerMessageID, update.Status, update.Event, payload, update.ErrorMessage,
		update.IncrementOpen, update.IncrementClick, update.MarkDelivered, nullableTime(update.OccurredAt),
	}
	scope := emailScopeCondition(update.AppID, &args)
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
WHERE provider_message_id = $1 AND `+scope, args...)
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
	args := make([]any, 0, 4)
	scope := emailScopeCondition(appID, &args)
	args = append(args, purpose, strings.TrimSpace(toAddress), since)
	var total int64
	query := fmt.Sprintf(`SELECT COUNT(*) FROM email_deliveries
WHERE %s AND purpose = $%d AND lower(to_address) = lower($%d) AND created_at >= $%d`,
		scope, len(args)-2, len(args)-1, len(args))
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// EmailDeliveryStats 统计一个作用域下的投递概况。
//
// 六个状态各查一次要打六次库，因此用一条带 FILTER 的聚合查询取齐 ——
// 这些数字是一起显示在同一条状态带上的，分开取会出现
// 「总数已刷新、退信数还是上一次」这种自相矛盾的画面。
func (r *Repository) EmailDeliveryStats(ctx context.Context, appID int64) (*emaildomain.DeliveryStats, error) {
	args := make([]any, 0, 1)
	scope := emailScopeCondition(appID, &args)
	var stats emaildomain.DeliveryStats
	err := r.pool.QueryRow(ctx, `SELECT
	COUNT(*),
	COUNT(*) FILTER (WHERE status = 'sent'),
	COUNT(*) FILTER (WHERE status = 'delivered'),
	COUNT(*) FILTER (WHERE status IN ('failed', 'rejected')),
	COUNT(*) FILTER (WHERE status IN ('bounced', 'complained')),
	COUNT(*) FILTER (WHERE status = 'pending'),
	COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '24 hours')
FROM email_deliveries WHERE `+scope, args...).
		Scan(&stats.Total, &stats.Sent, &stats.Delivered, &stats.Failed, &stats.Bounced, &stats.Pending, &stats.Last24h)
	if err != nil {
		return nil, err
	}
	return &stats, nil
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

	args := make([]any, 0, 8)
	conditions := []string{emailScopeCondition(query.AppID, &args)}
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
	if purpose := strings.TrimSpace(query.Purpose); purpose != "" {
		args = append(args, purpose)
		conditions = append(conditions, fmt.Sprintf("purpose = $%d", len(args)))
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

const emailDeliveryColumns = `id, COALESCE(appid, 0), COALESCE(config_id, 0), COALESCE(config_name, ''), provider, COALESCE(provider_message_id, ''), COALESCE(message_id, ''), to_address, COALESCE(from_address, ''), COALESCE(subject, ''), COALESCE(purpose, ''), status, COALESCE(error_message, ''), open_count, click_count, COALESCE(last_event, ''), COALESCE(last_event_payload, '{}'::jsonb), sent_at, delivered_at, created_at, updated_at`

func scanEmailDelivery(row interface{ Scan(dest ...any) error }) (*emaildomain.Delivery, error) {
	var item emaildomain.Delivery
	var payloadBytes []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.ConfigID, &item.ConfigName, &item.Provider,
		&item.ProviderMessageID, &item.MessageID, &item.ToAddress, &item.FromAddress, &item.Subject,
		&item.Purpose, &item.Status, &item.ErrorMessage, &item.OpenCount, &item.ClickCount,
		&item.LastEvent, &payloadBytes, &item.SentAt, &item.DeliveredAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	// Scope 是派生的，不单独存一列：多一列就多一个可能与 appid 对不上的事实。
	item.Scope = emaildomain.ScopeApp
	if item.AppID == emaildomain.PlatformAppID {
		item.Scope = emaildomain.ScopePlatform
	}
	if len(payloadBytes) > 0 {
		_ = json.Unmarshal(payloadBytes, &item.LastEventPayload)
	}
	return &item, nil
}
