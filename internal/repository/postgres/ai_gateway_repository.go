package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aegis/internal/domain/aigateway"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

// AI 中转网关的配置、模型、密钥、调用记录与统计。暂扣 / 结算 / 退款在 ai_gateway_billing_repository.go。

var (
	ErrAIGatewayModelNotFound   = errors.New("ai gateway: model not found")
	ErrAIGatewayModelDuplicate  = errors.New("ai gateway: model already exists")
	ErrAIGatewayKeyNotFound     = errors.New("ai gateway: key not found")
	ErrAIGatewayKeyQuota        = errors.New("ai gateway: too many keys")
	ErrAIGatewayRequestNotFound = errors.New("ai gateway: request not found")
)

// ── 设置 ──

// GetAIGatewaySettings 应用的网关设置；没保存过返回默认值。
// 存量 JSON 解到默认值上：以后新增的字段对老配置自动取默认，不会是零值。
func (r *Repository) GetAIGatewaySettings(ctx context.Context, appID int64) (*aigateway.Settings, error) {
	settings := aigateway.DefaultSettings()
	var raw []byte
	var updatedBy *string
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT settings, updated_by, updated_at FROM ai_gateway_settings WHERE appid = $1`, appID).
		Scan(&raw, &updatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &settings, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("decode ai gateway settings: %w", err)
	}
	if updatedBy != nil {
		settings.UpdatedBy = *updatedBy
	}
	settings.UpdatedAt = &updatedAt
	return &settings, nil
}

// SaveAIGatewaySettings 保存设置（调用方已校验）。
func (r *Repository) SaveAIGatewaySettings(ctx context.Context, appID int64, settings aigateway.Settings, operator string) (*aigateway.Settings, error) {
	settings.UpdatedBy = ""
	settings.UpdatedAt = nil
	raw, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO ai_gateway_settings (appid, settings, updated_by, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (appid) DO UPDATE SET settings = EXCLUDED.settings, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		appID, raw, nullableString(operator)); err != nil {
		return nil, err
	}
	return r.GetAIGatewaySettings(ctx, appID)
}

// ── 模型 ──

const aiGatewayModelColumns = `id, appid, model, upstream_model, kind, display_name, description, config_id, enabled, vip_only,
free, input_price::text, output_price::text, request_price::text, image_price::text, image_prices, char_price::text,
minute_price::text, max_output_tokens, capabilities, sort_order, created_at, updated_at`

func scanAIGatewayModel(row interface{ Scan(dest ...any) error }) (*aigateway.Model, error) {
	var item aigateway.Model
	var input, output, request, image, char, minute string
	var imagePrices, capabilities []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Model, &item.UpstreamModel, &item.Kind, &item.DisplayName,
		&item.Description, &item.ConfigID, &item.Enabled, &item.VipOnly, &item.Free, &input, &output, &request, &image,
		&imagePrices, &char, &minute, &item.MaxOutputTokens, &capabilities, &item.SortOrder, &item.CreatedAt,
		&item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAIGatewayModelNotFound
		}
		return nil, err
	}
	item.InputPrice = decimal.RequireFromString(input)
	item.OutputPrice = decimal.RequireFromString(output)
	item.RequestPrice = decimal.RequireFromString(request)
	item.ImagePrice = decimal.RequireFromString(image)
	item.CharPrice = decimal.RequireFromString(char)
	item.MinutePrice = decimal.RequireFromString(minute)
	item.Capabilities = aigateway.DefaultCapabilities()
	if len(capabilities) > 0 {
		if err := json.Unmarshal(capabilities, &item.Capabilities); err != nil {
			return nil, fmt.Errorf("decode model capabilities: %w", err)
		}
		if item.Capabilities.ReasoningEfforts == nil {
			item.Capabilities.ReasoningEfforts = []string{}
		}
	}
	if len(imagePrices) > 0 {
		if err := json.Unmarshal(imagePrices, &item.ImagePrices); err != nil {
			return nil, fmt.Errorf("decode image prices: %w", err)
		}
		if len(item.ImagePrices) == 0 {
			item.ImagePrices = nil
		}
	}
	return &item, nil
}

// ListAIGatewayModels 应用的模型；enabledOnly 只要启用的。
func (r *Repository) ListAIGatewayModels(ctx context.Context, appID int64, enabledOnly bool) ([]aigateway.Model, error) {
	query := `SELECT ` + aiGatewayModelColumns + ` FROM ai_gateway_models WHERE appid = $1`
	if enabledOnly {
		query += ` AND enabled`
	}
	rows, err := r.pool.Query(ctx, query+` ORDER BY sort_order ASC, id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]aigateway.Model, 0, 8)
	for rows.Next() {
		item, err := scanAIGatewayModel(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetAIGatewayModelByName 按对外名取模型（含停用的，由调用方判断）。
func (r *Repository) GetAIGatewayModelByName(ctx context.Context, appID int64, model string) (*aigateway.Model, error) {
	return scanAIGatewayModel(r.pool.QueryRow(ctx,
		`SELECT `+aiGatewayModelColumns+` FROM ai_gateway_models WHERE appid = $1 AND model = $2`, appID, model))
}

// GetAIGatewayModel 按 ID 取模型。
func (r *Repository) GetAIGatewayModel(ctx context.Context, appID, modelID int64) (*aigateway.Model, error) {
	return scanAIGatewayModel(r.pool.QueryRow(ctx,
		`SELECT `+aiGatewayModelColumns+` FROM ai_gateway_models WHERE appid = $1 AND id = $2`, appID, modelID))
}

func aiGatewayModelArgs(m aigateway.Model) ([]any, error) {
	imagePrices := []byte("{}")
	if len(m.ImagePrices) > 0 {
		encoded, err := json.Marshal(m.ImagePrices)
		if err != nil {
			return nil, err
		}
		imagePrices = encoded
	}
	capabilities, err := json.Marshal(m.Capabilities)
	if err != nil {
		return nil, err
	}
	return []any{m.Model, m.UpstreamModel, m.Kind, m.DisplayName, m.Description, m.ConfigID, m.Enabled, m.VipOnly,
		m.Free, m.InputPrice.String(), m.OutputPrice.String(), m.RequestPrice.String(), m.ImagePrice.String(),
		imagePrices, m.CharPrice.String(), m.MinutePrice.String(), m.MaxOutputTokens, m.SortOrder, capabilities}, nil
}

func mapAIGatewayModelWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrAIGatewayModelDuplicate
	}
	return err
}

// CreateAIGatewayModel 新建模型（已校验）。
func (r *Repository) CreateAIGatewayModel(ctx context.Context, appID int64, m aigateway.Model) (*aigateway.Model, error) {
	args, err := aiGatewayModelArgs(m)
	if err != nil {
		return nil, err
	}
	item, err := scanAIGatewayModel(r.pool.QueryRow(ctx, `INSERT INTO ai_gateway_models
(model, upstream_model, kind, display_name, description, config_id, enabled, vip_only, free, input_price, output_price,
 request_price, image_price, image_prices, char_price, minute_price, max_output_tokens, sort_order, capabilities, appid)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
RETURNING `+aiGatewayModelColumns, append(args, appID)...))
	if err != nil {
		return nil, mapAIGatewayModelWriteError(err)
	}
	return item, nil
}

// UpdateAIGatewayModel 修改模型（已校验）。
func (r *Repository) UpdateAIGatewayModel(ctx context.Context, appID, modelID int64, m aigateway.Model) (*aigateway.Model, error) {
	args, err := aiGatewayModelArgs(m)
	if err != nil {
		return nil, err
	}
	item, err := scanAIGatewayModel(r.pool.QueryRow(ctx, `UPDATE ai_gateway_models SET
model = $1, upstream_model = $2, kind = $3, display_name = $4, description = $5, config_id = $6, enabled = $7,
vip_only = $8, free = $9, input_price = $10, output_price = $11, request_price = $12, image_price = $13,
image_prices = $14, char_price = $15, minute_price = $16, max_output_tokens = $17, sort_order = $18,
capabilities = $19, updated_at = NOW()
WHERE appid = $20 AND id = $21
RETURNING `+aiGatewayModelColumns, append(args, appID, modelID)...))
	if err != nil {
		return nil, mapAIGatewayModelWriteError(err)
	}
	return item, nil
}

// DeleteAIGatewayModel 删除模型。进行中的调用按暂扣时的价格快照结算，不受影响。
func (r *Repository) DeleteAIGatewayModel(ctx context.Context, appID, modelID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ai_gateway_models WHERE appid = $1 AND id = $2`, appID, modelID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAIGatewayModelNotFound
	}
	return nil
}

// ── 密钥 ──

const aiGatewayKeyColumns = `k.id, k.appid, k.user_id, k.name, k.key_hint, k.status, k.models, k.spend_limit, k.spent,
k.reserved, k.expires_at, k.last_used_at, COALESCE(k.last_used_ip, ''), k.created_at, k.updated_at`

func scanAIGatewayKey(row interface{ Scan(dest ...any) error }, extra ...any) (*aigateway.Key, error) {
	var item aigateway.Key
	dest := []any{&item.ID, &item.AppID, &item.UserID, &item.Name, &item.Hint, &item.Status, &item.Models,
		&item.SpendLimitMicro, &item.SpentMicro, &item.ReservedMicro, &item.ExpiresAt, &item.LastUsedAt,
		&item.LastUsedIP, &item.CreatedAt, &item.UpdatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAIGatewayKeyNotFound
		}
		return nil, err
	}
	fillAIGatewayKeyAmounts(&item)
	return &item, nil
}

func fillAIGatewayKeyAmounts(item *aigateway.Key) {
	if item.Models == nil {
		item.Models = []string{}
	}
	item.SpendLimit = aigateway.MicroToPoints(item.SpendLimitMicro)
	item.Spent = aigateway.MicroToPoints(item.SpentMicro)
}

// CreateAIGatewayKey 新建密钥：锁用户行后数一遍再插，并发创建也不会超过上限。maxKeys 为 0 表示不允许创建。
func (r *Repository) CreateAIGatewayKey(ctx context.Context, key aigateway.Key, hash string, maxKeys int) (*aigateway.Key, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var userID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, key.UserID, key.AppID).
		Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_keys WHERE appid = $1 AND user_id = $2 AND status <> 'revoked'`,
		key.AppID, key.UserID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= maxKeys {
		return nil, ErrAIGatewayKeyQuota
	}
	item, err := scanAIGatewayKey(tx.QueryRow(ctx, `INSERT INTO ai_gateway_keys AS k
(appid, user_id, name, key_hash, key_hint, status, models, spend_limit, expires_at)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8)
RETURNING `+aiGatewayKeyColumns,
		key.AppID, key.UserID, key.Name, hash, key.Hint, key.Models, key.SpendLimitMicro, key.ExpiresAt))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return item, nil
}

// ListAIGatewayUserKeys 某个用户的密钥（不含已删除的）。
func (r *Repository) ListAIGatewayUserKeys(ctx context.Context, appID, userID int64) ([]aigateway.Key, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+aiGatewayKeyColumns+` FROM ai_gateway_keys k
WHERE k.appid = $1 AND k.user_id = $2 AND k.status <> 'revoked' ORDER BY k.id DESC`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]aigateway.Key, 0, 4)
	for rows.Next() {
		item, err := scanAIGatewayKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ListAIGatewayKeys 管理端：全应用的密钥，可按用户与状态筛。
func (r *Repository) ListAIGatewayKeys(ctx context.Context, appID, userID int64, status string, page, limit int) (*aigateway.Page[aigateway.Key], error) {
	page, limit = aiGatewayPageLimit(page, limit, 100)
	args := []any{appID}
	where := []string{"k.appid = $1"}
	if userID > 0 {
		args = append(args, userID)
		where = append(where, fmt.Sprintf("k.user_id = $%d", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("k.status = $%d", len(args)))
	} else {
		where = append(where, "k.status <> 'revoked'")
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_keys k WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+aiGatewayKeyColumns+`, COALESCE(u.account, '') FROM ai_gateway_keys k
LEFT JOIN users u ON u.id = k.user_id
WHERE `+clause+fmt.Sprintf(` ORDER BY k.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]aigateway.Key, 0, limit)
	for rows.Next() {
		var account string
		item, err := scanAIGatewayKey(rows, &account)
		if err != nil {
			return nil, err
		}
		item.Account = account
		items = append(items, *item)
	}
	return &aigateway.Page[aigateway.Key]{Items: items, Total: total, Page: page, Limit: limit}, rows.Err()
}

// GetAIGatewayKey 按 ID 取密钥。userID 为 0 时不校验持有人（管理端）。
func (r *Repository) GetAIGatewayKey(ctx context.Context, appID, userID, keyID int64) (*aigateway.Key, error) {
	item, err := scanAIGatewayKey(r.pool.QueryRow(ctx, `SELECT `+aiGatewayKeyColumns+` FROM ai_gateway_keys k
WHERE k.appid = $1 AND k.id = $2 AND k.status <> 'revoked'`, appID, keyID))
	if err != nil {
		return nil, err
	}
	if userID > 0 && item.UserID != userID {
		return nil, ErrAIGatewayKeyNotFound
	}
	return item, nil
}

// FindAIGatewayKeyByHash 凭散列找密钥（鉴权用，含停用的，由调用方判断能否使用）。
func (r *Repository) FindAIGatewayKeyByHash(ctx context.Context, hash string) (*aigateway.Key, error) {
	return scanAIGatewayKey(r.pool.QueryRow(ctx, `SELECT `+aiGatewayKeyColumns+` FROM ai_gateway_keys k
WHERE k.key_hash = $1 AND k.status <> 'revoked'`, hash))
}

// UpdateAIGatewayKey 修改密钥（已校验）：只改传了的字段。userID 为 0 时不校验持有人。
func (r *Repository) UpdateAIGatewayKey(ctx context.Context, appID, userID, keyID int64, in aigateway.KeyInput) (*aigateway.Key, error) {
	current, err := r.GetAIGatewayKey(ctx, appID, userID, keyID)
	if err != nil {
		return nil, err
	}
	name, models, spendLimit := current.Name, current.Models, current.SpendLimitMicro
	if in.Name != nil && *in.Name != "" {
		name = *in.Name
	}
	if in.Models != nil {
		models = *in.Models
	}
	if in.SpendLimit != nil {
		spendLimit = aigateway.PointsToMicroFloor(*in.SpendLimit)
	}
	status := current.Status
	if in.Status != "" {
		status = in.Status
	}
	expiresAt := current.ExpiresAt
	if in.ClearExpiry {
		expiresAt = nil
	} else if in.ExpiresAt != nil {
		expiresAt = in.ExpiresAt
	}
	return scanAIGatewayKey(r.pool.QueryRow(ctx, `UPDATE ai_gateway_keys AS k
SET name = $3, status = $4, models = $5, spend_limit = $6, expires_at = $7, updated_at = NOW()
WHERE k.appid = $1 AND k.id = $2 AND k.status <> 'revoked'
RETURNING `+aiGatewayKeyColumns,
		appID, keyID, name, status, models, spendLimit, expiresAt))
}

// SetAIGatewayKeyStatus 管理端改状态（启用 / 停用）。
func (r *Repository) SetAIGatewayKeyStatus(ctx context.Context, appID, keyID int64, status string) (*aigateway.Key, error) {
	return scanAIGatewayKey(r.pool.QueryRow(ctx, `UPDATE ai_gateway_keys AS k SET status = $3, updated_at = NOW()
WHERE k.appid = $1 AND k.id = $2 AND k.status <> 'revoked'
RETURNING `+aiGatewayKeyColumns, appID, keyID, status))
}

// RevokeAIGatewayKey 删除密钥（软删）。进行中的调用照常结算。
func (r *Repository) RevokeAIGatewayKey(ctx context.Context, appID, userID, keyID int64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE ai_gateway_keys SET status = 'revoked', updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND id = $3 AND status <> 'revoked'`, appID, userID, keyID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAIGatewayKeyNotFound
	}
	return nil
}

// ── 调用记录 ──

const aiGatewayRequestColumns = `r.id, r.request_no, r.appid, r.user_id, r.key_id, r.key_hint, r.auth_kind, r.endpoint, r.model,
r.upstream_model, r.config_id, r.provider, r.stream, r.status, r.price, r.factor::text, r.estimate, r.reserved, r.holds,
r.billing, r.clamp_max_tokens, r.input_tokens, r.output_tokens, r.images, r.characters, r.audio_ms, r.usage_estimated,
r.cost, r.charged, r.shortfall, r.charges, COALESCE(r.integral_txn_no, ''), COALESCE(r.wallet_txn_no, ''),
r.error_code, r.error_message, COALESCE(r.client_ip, ''), COALESCE(r.user_agent, ''), r.latency_ms,
COALESCE(r.refund_reason, ''), COALESCE(r.refunded_by, ''), r.refunded_at, r.created_at, r.expires_at, r.settled_at,
r.attempts, r.first_token_ms, r.finish_reason, r.upstream_request_id, r.request_bytes, r.response_bytes,
COALESCE((SELECT c.name FROM ai_provider_configs c WHERE c.id = r.config_id), '')`

func scanAIGatewayRequest(row interface{ Scan(dest ...any) error }, extra ...any) (*aigateway.Request, error) {
	var item aigateway.Request
	var price, holds, billing, charges []byte
	var factor string
	dest := []any{&item.ID, &item.RequestNo, &item.AppID, &item.UserID, &item.KeyID, &item.KeyHint, &item.AuthKind,
		&item.Endpoint, &item.Model, &item.UpstreamModel, &item.ConfigID, &item.Provider, &item.Stream, &item.Status,
		&price, &factor, &item.EstimateMicro, &item.ReservedMicro, &holds, &billing, &item.ClampMaxTokens,
		&item.InputTokens, &item.OutputTokens, &item.Images, &item.Characters, &item.AudioMillis, &item.UsageEstimated,
		&item.CostMicro, &item.ChargedMicro, &item.ShortfallMicro, &charges, &item.IntegralTxnNo, &item.WalletTxnNo,
		&item.ErrorCode, &item.ErrorMessage, &item.ClientIP, &item.UserAgent, &item.LatencyMs, &item.RefundReason,
		&item.RefundedBy, &item.RefundedAt, &item.CreatedAt, &item.ExpiresAt, &item.SettledAt,
		&item.Attempts, &item.FirstTokenMs, &item.FinishReason, &item.UpstreamRequestID, &item.RequestBytes,
		&item.ResponseBytes, &item.ConfigName}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAIGatewayRequestNotFound
		}
		return nil, err
	}
	for _, field := range []struct {
		raw    []byte
		target any
	}{{price, &item.Price}, {holds, &item.Holds}, {billing, &item.Billing}, {charges, &item.Charges}} {
		if len(field.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(field.raw, field.target); err != nil {
			return nil, fmt.Errorf("decode ai gateway request %s: %w", item.RequestNo, err)
		}
	}
	if item.Holds == nil {
		item.Holds = []aigateway.Hold{}
	}
	if item.Charges == nil {
		item.Charges = []aigateway.Charge{}
	}
	item.Factor = decimal.RequireFromString(factor)
	item.Estimate = aigateway.MicroToPoints(item.EstimateMicro)
	item.Reserved = aigateway.MicroToPoints(item.ReservedMicro)
	item.Cost = aigateway.MicroToPoints(item.CostMicro)
	item.Charged = aigateway.MicroToPoints(item.ChargedMicro)
	item.Shortfall = aigateway.MicroToPoints(item.ShortfallMicro)
	return &item, nil
}

// GetAIGatewayRequest 按编号取记录。userID 非 0 时校验归属（用户端）。
func (r *Repository) GetAIGatewayRequest(ctx context.Context, appID, userID int64, requestNo string) (*aigateway.Request, error) {
	var account string
	item, err := scanAIGatewayRequest(r.pool.QueryRow(ctx, `SELECT `+aiGatewayRequestColumns+`, COALESCE(u.account, '')
FROM ai_gateway_requests r LEFT JOIN users u ON u.id = r.user_id
WHERE r.appid = $1 AND r.request_no = $2`, appID, requestNo), &account)
	if err != nil {
		return nil, err
	}
	if userID > 0 && item.UserID != userID {
		return nil, ErrAIGatewayRequestNotFound
	}
	item.Account = account
	return item, nil
}

// ListAIGatewayRequests 调用记录分页。
func (r *Repository) ListAIGatewayRequests(ctx context.Context, query aigateway.RequestQuery) (*aigateway.Page[aigateway.Request], error) {
	page, limit := aiGatewayPageLimit(query.Page, query.Limit, 100)
	args := []any{query.AppID}
	where := []string{"r.appid = $1"}
	add := func(cond string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if query.UserID > 0 {
		add("r.user_id = $%d", query.UserID)
	}
	if query.KeyID > 0 {
		add("r.key_id = $%d", query.KeyID)
	}
	if query.Model != "" {
		add("r.model = $%d", query.Model)
	}
	if query.Status != "" {
		add("r.status = $%d", query.Status)
	}
	if query.Endpoint != "" {
		add("r.endpoint = $%d", query.Endpoint)
	}
	if query.ConfigID > 0 {
		add("r.config_id = $%d", query.ConfigID)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		pattern := escapeLikePattern(keyword) + "%"
		args = append(args, pattern)
		where = append(where, fmt.Sprintf("(r.request_no LIKE $%d OR r.upstream_request_id LIKE $%d)", len(args), len(args)))
	}
	if query.From != nil {
		add("r.created_at >= $%d", *query.From)
	}
	if query.To != nil {
		add("r.created_at < $%d", *query.To)
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_requests r WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+aiGatewayRequestColumns+`, COALESCE(u.account, '')
FROM ai_gateway_requests r LEFT JOIN users u ON u.id = r.user_id
WHERE `+clause+fmt.Sprintf(` ORDER BY r.created_at DESC, r.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]aigateway.Request, 0, limit)
	for rows.Next() {
		var account string
		item, err := scanAIGatewayRequest(rows, &account)
		if err != nil {
			return nil, err
		}
		item.Account = account
		items = append(items, *item)
	}
	return &aigateway.Page[aigateway.Request]{Items: items, Total: total, Page: page, Limit: limit}, rows.Err()
}

// ── 账户与额度 ──

// AIGatewayAccountFacts 用户总览要的全部事实：会员与等级、积分、余额、网关账户、当前周期的额度用量。
func (r *Repository) AIGatewayAccountFacts(ctx context.Context, appID, userID int64, settings aigateway.Settings, now time.Time) (*aigateway.AccountFacts, error) {
	facts := aigateway.AccountFacts{
		Account:    aigateway.Account{AppID: appID, UserID: userID},
		QuotaUsage: map[string]aigateway.QuotaUsage{},
	}
	var vipExpire, lifetime *time.Time
	var walletBalance string
	err := r.pool.QueryRow(ctx, `SELECT u.integral, u.vip_expire_at, u.vip_lifetime_at,
  COALESCE((SELECT l.current_level FROM user_level_records l WHERE l.user_id = u.id AND l.appid = u.appid), 1),
  COALESCE((SELECT w.balance::text FROM user_wallets w WHERE w.user_id = u.id AND w.appid = u.appid), '0'),
  COALESCE(a.credit, 0), COALESCE(a.credit_frozen, 0), COALESCE(a.integral_carry, 0), COALESCE(a.wallet_carry, 0),
  COALESCE(a.total_spent, 0), COALESCE(a.total_requests, 0), a.welcome_claimed_at
FROM users u LEFT JOIN ai_gateway_accounts a ON a.user_id = u.id
WHERE u.id = $1 AND u.appid = $2`, userID, appID).Scan(&facts.Integral, &vipExpire, &lifetime, &facts.Profile.Level,
		&walletBalance, &facts.Account.Credit, &facts.Account.CreditFrozen, &facts.Account.IntegralCarry,
		&facts.Account.WalletCarry, &facts.Account.TotalSpent, &facts.Account.TotalRequests, &facts.WelcomeClaimedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	profile, err := aiGatewayProfileQ(ctx, r.pool, appID, userID, isVipAt(vipExpire, lifetime, now))
	if err != nil {
		return nil, err
	}
	facts.Profile = profile
	facts.WalletCents = decimal.RequireFromString(walletBalance).Shift(2).Floor().IntPart()
	// 额度包：可用、暂扣中、7 天内到期的部分与最早的到期时间（暂扣口径与 lockUsableAIGatewayLotsTx 一致）。
	if err := r.pool.QueryRow(ctx, `SELECT
  COALESCE(SUM(remaining), 0)::bigint, COALESCE(SUM(frozen), 0)::bigint,
  COALESCE(SUM(remaining) FILTER (WHERE expires_at <= $3::timestamptz + INTERVAL '7 days'), 0)::bigint,
  MIN(expires_at) FILTER (WHERE remaining > 0 AND expires_at <= $3::timestamptz + INTERVAL '7 days')
FROM ai_gateway_credit_lots
WHERE user_id = $1 AND appid = $2 AND status = 'active' AND (expires_at IS NULL OR expires_at > $3)`, userID, appID, now).
		Scan(&facts.Pack, &facts.PackFrozen, &facts.ExpiringMicro, &facts.ExpiringAt); err != nil {
		return nil, err
	}

	loc := settings.Location()
	keys := map[string]string{}
	for _, source := range []string{aigateway.SourceFreeDaily, aigateway.SourceVipDaily, aigateway.SourceVipMonthly, aigateway.SpendDaily} {
		keys[source] = aigateway.PeriodKey(source, now, loc)
	}
	rows, err := r.pool.Query(ctx, `SELECT source, period_key, used, reserved FROM ai_gateway_quota_usage
WHERE user_id = $1 AND (source, period_key) IN (($2, $3), ($4, $5), ($6, $7), ($8, $9))`, userID,
		aigateway.SourceFreeDaily, keys[aigateway.SourceFreeDaily], aigateway.SourceVipDaily, keys[aigateway.SourceVipDaily],
		aigateway.SourceVipMonthly, keys[aigateway.SourceVipMonthly], aigateway.SpendDaily, keys[aigateway.SpendDaily])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		var usage aigateway.QuotaUsage
		if err := rows.Scan(&source, &usage.Period, &usage.Used, &usage.Reserved); err != nil {
			return nil, err
		}
		facts.QuotaUsage[source] = usage
	}
	for source, period := range keys {
		if _, ok := facts.QuotaUsage[source]; !ok {
			facts.QuotaUsage[source] = aigateway.QuotaUsage{Period: period}
		}
	}
	return &facts, rows.Err()
}

func isVipAt(expireAt, lifetimeSince *time.Time, now time.Time) bool {
	return lifetimeSince != nil || (expireAt != nil && expireAt.After(now))
}

// ListAIGatewayCredits 赠送额度流水。
func (r *Repository) ListAIGatewayCredits(ctx context.Context, appID, userID int64, page, limit int) (*aigateway.Page[aigateway.CreditEntry], error) {
	page, limit = aiGatewayPageLimit(page, limit, 100)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_credit_ledger WHERE appid = $1 AND user_id = $2`,
		appID, userID).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id, kind, bucket, COALESCE(lot_id, 0), amount, balance_after, COALESCE(total_after, balance_after),
ref_type, ref_id, remark, COALESCE(operator, ''), created_at
FROM ai_gateway_credit_ledger WHERE appid = $1 AND user_id = $2 ORDER BY id DESC LIMIT $3 OFFSET $4`,
		appID, userID, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]aigateway.CreditEntry, 0, limit)
	for rows.Next() {
		var item aigateway.CreditEntry
		var amount, balance, totalAfter int64
		if err := rows.Scan(&item.ID, &item.Kind, &item.Bucket, &item.LotID, &amount, &balance, &totalAfter, &item.RefType,
			&item.RefID, &item.Remark, &item.Operator, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Amount = aigateway.MicroToPoints(amount)
		item.BalanceAfter = aigateway.MicroToPoints(balance)
		item.TotalAfter = aigateway.MicroToPoints(totalAfter)
		items = append(items, item)
	}
	return &aigateway.Page[aigateway.CreditEntry]{Items: items, Total: total, Page: page, Limit: limit}, rows.Err()
}

// ── 统计 ──

// AIGatewayStats 最近 days 天的统计；按日分桶用 timezone。
func (r *Repository) AIGatewayStats(ctx context.Context, appID int64, days int, timezone string, now time.Time) (*aigateway.Stats, error) {
	if days < 1 {
		days = 1
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
		timezone = "UTC"
	}
	localNow := now.In(loc)
	since := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	stats := aigateway.Stats{Days: days, BySource: []aigateway.SourceTotal{}, ByModel: []aigateway.ModelTotal{}, Daily: []aigateway.DailyTotal{}}

	var charged, shortfall, refunded int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*),
  COUNT(*) FILTER (WHERE status IN ('succeeded', 'aborted', 'refunded')),
  COUNT(*) FILTER (WHERE status IN ('failed', 'expired')),
  COUNT(DISTINCT user_id),
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(images), 0),
  COALESCE(SUM(charged) FILTER (WHERE status IN ('succeeded', 'aborted')), 0),
  COALESCE(SUM(shortfall) FILTER (WHERE status IN ('succeeded', 'aborted')), 0),
  COALESCE(SUM(charged) FILTER (WHERE status = 'refunded'), 0)
FROM ai_gateway_requests WHERE appid = $1 AND created_at >= $2`, appID, since).Scan(&stats.Requests, &stats.Succeeded,
		&stats.Failed, &stats.Users, &stats.InputTokens, &stats.OutputTokens, &stats.Images, &charged, &shortfall, &refunded); err != nil {
		return nil, err
	}
	stats.Charged = aigateway.MicroToPoints(charged)
	stats.Shortfall = aigateway.MicroToPoints(shortfall)
	stats.Refunded = aigateway.MicroToPoints(refunded)

	rows, err := r.pool.Query(ctx, `SELECT c->>'source', COALESCE(SUM((c->>'micro')::bigint), 0), COALESCE(SUM((c->>'units')::bigint), 0)
FROM ai_gateway_requests r, jsonb_array_elements(r.charges) c
WHERE r.appid = $1 AND r.created_at >= $2 AND r.status IN ('succeeded', 'aborted')
GROUP BY 1 ORDER BY 2 DESC`, appID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item aigateway.SourceTotal
		var micro int64
		if err := rows.Scan(&item.Source, &micro, &item.Units); err != nil {
			rows.Close()
			return nil, err
		}
		item.Amount = aigateway.MicroToPoints(micro)
		stats.BySource = append(stats.BySource, item)
	}
	rows.Close()

	rows, err = r.pool.Query(ctx, `SELECT model, COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(charged) FILTER (WHERE status IN ('succeeded', 'aborted')), 0)
FROM ai_gateway_requests WHERE appid = $1 AND created_at >= $2
GROUP BY model ORDER BY 5 DESC, 2 DESC LIMIT 20`, appID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item aigateway.ModelTotal
		var micro int64
		if err := rows.Scan(&item.Model, &item.Requests, &item.InputTokens, &item.OutputTokens, &micro); err != nil {
			rows.Close()
			return nil, err
		}
		item.Charged = aigateway.MicroToPoints(micro)
		stats.ByModel = append(stats.ByModel, item)
	}
	rows.Close()

	rows, err = r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD'), COUNT(*),
  COUNT(*) FILTER (WHERE status IN ('failed', 'expired')),
  COALESCE(SUM(charged) FILTER (WHERE status IN ('succeeded', 'aborted')), 0)
FROM ai_gateway_requests WHERE appid = $1 AND created_at >= $2
GROUP BY 1 ORDER BY 1`, appID, since, timezone)
	if err != nil {
		return nil, err
	}
	byDate := map[string]aigateway.DailyTotal{}
	for rows.Next() {
		var item aigateway.DailyTotal
		var micro int64
		if err := rows.Scan(&item.Date, &item.Requests, &item.Failed, &micro); err != nil {
			rows.Close()
			return nil, err
		}
		item.Charged = aigateway.MicroToPoints(micro)
		byDate[item.Date] = item
	}
	rows.Close()
	for i := 0; i < days; i++ {
		date := since.AddDate(0, 0, i).Format("2006-01-02")
		item, ok := byDate[date]
		if !ok {
			item = aigateway.DailyTotal{Date: date, Charged: decimal.Zero}
		}
		stats.Daily = append(stats.Daily, item)
	}

	var credit int64
	if err := r.pool.QueryRow(ctx, `SELECT
  (SELECT COUNT(*) FROM ai_gateway_requests WHERE appid = $1 AND status = 'reserved'),
  (SELECT COALESCE(SUM(credit + credit_frozen), 0) FROM ai_gateway_accounts WHERE appid = $1)`, appID).
		Scan(&stats.ReservedNow, &credit); err != nil {
		return nil, err
	}
	stats.CreditBalance = aigateway.MicroToPoints(credit)
	return &stats, nil
}

// aiGatewayPageLimit 分页参数归一：page 从 1 起，limit 在 1-max 之间，默认 20。
func aiGatewayPageLimit(page, limit, max int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > max {
		limit = max
	}
	return page, limit
}

// AIGatewayTodaySpent 用户今天（按设置的时区）实际扣到的点数（微点），含中途断开的调用。
func (r *Repository) AIGatewayTodaySpent(ctx context.Context, appID, userID int64, settings aigateway.Settings, now time.Time) (int64, error) {
	local := now.In(settings.Location())
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	var spent int64
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(charged), 0) FROM ai_gateway_requests
WHERE appid = $1 AND user_id = $2 AND created_at >= $3 AND status IN ('succeeded', 'aborted')`, appID, userID, dayStart).Scan(&spent)
	return spent, err
}
