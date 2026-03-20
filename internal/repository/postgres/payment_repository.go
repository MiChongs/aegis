package postgres

import (
	paymentdomain "aegis/internal/domain/payment"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

func (r *Repository) ListPaymentConfigs(ctx context.Context, appID int64, paymentMethod string, enabledOnly bool) ([]paymentdomain.Config, error) {
	args := []any{appID}
	query := `SELECT id, appid, payment_method, config_name, COALESCE(config_data, '{}'::jsonb), enabled, is_default, COALESCE(description, ''), created_at, updated_at FROM payment_configs WHERE appid = $1`
	if paymentMethod = strings.TrimSpace(paymentMethod); paymentMethod != "" {
		query += fmt.Sprintf(" AND payment_method = $%d", len(args)+1)
		args = append(args, paymentMethod)
	}
	if enabledOnly {
		query += " AND enabled = TRUE"
	}
	query += " ORDER BY is_default DESC, id ASC"
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]paymentdomain.Config, 0, 4)
	for rows.Next() {
		item, err := scanPaymentConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetPaymentConfigByID(ctx context.Context, appID int64, id int64) (*paymentdomain.Config, error) {
	return scanPaymentConfig(r.pool.QueryRow(ctx, `SELECT id, appid, payment_method, config_name, COALESCE(config_data, '{}'::jsonb), enabled, is_default, COALESCE(description, ''), created_at, updated_at FROM payment_configs WHERE appid = $1 AND id = $2 LIMIT 1`, appID, id))
}

func (r *Repository) GetPaymentConfig(ctx context.Context, appID int64, paymentMethod string, configName string) (*paymentdomain.Config, error) {
	if strings.TrimSpace(configName) != "" {
		return scanPaymentConfig(r.pool.QueryRow(ctx, `SELECT id, appid, payment_method, config_name, COALESCE(config_data, '{}'::jsonb), enabled, is_default, COALESCE(description, ''), created_at, updated_at FROM payment_configs WHERE appid = $1 AND payment_method = $2 AND config_name = $3 LIMIT 1`, appID, paymentMethod, configName))
	}
	return scanPaymentConfig(r.pool.QueryRow(ctx, `SELECT id, appid, payment_method, config_name, COALESCE(config_data, '{}'::jsonb), enabled, is_default, COALESCE(description, ''), created_at, updated_at FROM payment_configs WHERE appid = $1 AND payment_method = $2 AND enabled = TRUE ORDER BY is_default DESC, id ASC LIMIT 1`, appID, paymentMethod))
}

func (r *Repository) UpsertPaymentConfig(ctx context.Context, item paymentdomain.Config) (*paymentdomain.Config, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if item.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE payment_configs SET is_default = FALSE, updated_at = NOW() WHERE appid = $1 AND payment_method = $2 AND id <> $3`, item.AppID, item.PaymentMethod, item.ID); err != nil {
			return nil, err
		}
	}
	data, _ := json.Marshal(item.ConfigData)
	query := `INSERT INTO payment_configs (id, appid, payment_method, config_name, config_data, enabled, is_default, description, created_at, updated_at)
VALUES (NULLIF($1, 0), $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
	payment_method = EXCLUDED.payment_method,
	config_name = EXCLUDED.config_name,
	config_data = EXCLUDED.config_data,
	enabled = EXCLUDED.enabled,
	is_default = EXCLUDED.is_default,
	description = EXCLUDED.description,
	updated_at = NOW()
RETURNING id, appid, payment_method, config_name, COALESCE(config_data, '{}'::jsonb), enabled, is_default, COALESCE(description, ''), created_at, updated_at`
	saved, err := scanPaymentConfig(tx.QueryRow(ctx, query, item.ID, item.AppID, item.PaymentMethod, item.ConfigName, data, item.Enabled, item.IsDefault, nullableString(item.Description)))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

func (r *Repository) DeletePaymentConfig(ctx context.Context, appID int64, id int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM payment_configs WHERE appid = $1 AND id = $2`, appID, id)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *Repository) CreatePaymentOrder(ctx context.Context, item paymentdomain.OrderMutation) (*paymentdomain.Order, error) {
	meta, _ := json.Marshal(item.Metadata)
	query := `INSERT INTO payment_orders (appid, user_id, config_id, order_no, subject, body, amount, payment_method, provider_type, status, notify_status, client_ip, notify_url, return_url, metadata, expire_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', 'pending', $10, $11, $12, $13, $14, NOW(), NOW())
RETURNING id, appid, user_id, config_id, order_no, COALESCE(provider_order_no, ''), subject, COALESCE(body, ''), amount, payment_method, provider_type, status, notify_status, COALESCE(client_ip, ''), COALESCE(notify_url, ''), COALESCE(return_url, ''), COALESCE(metadata, '{}'::jsonb), COALESCE(raw_callback, '{}'::jsonb), paid_at, expire_at, created_at, updated_at`
	return scanPaymentOrder(r.pool.QueryRow(ctx, query, item.AppID, item.UserID, item.ConfigID, item.OrderNo, item.Subject, nullableString(item.Body), item.Amount.StringFixed(2), item.PaymentMethod, item.ProviderType, nullableString(item.ClientIP), nullableString(item.NotifyURL), nullableString(item.ReturnURL), meta, item.ExpireAt))
}

func (r *Repository) GetPaymentOrderByOrderNo(ctx context.Context, orderNo string) (*paymentdomain.Order, error) {
	query := `SELECT id, appid, user_id, config_id, order_no, COALESCE(provider_order_no, ''), subject, COALESCE(body, ''), amount, payment_method, provider_type, status, notify_status, COALESCE(client_ip, ''), COALESCE(notify_url, ''), COALESCE(return_url, ''), COALESCE(metadata, '{}'::jsonb), COALESCE(raw_callback, '{}'::jsonb), paid_at, expire_at, created_at, updated_at FROM payment_orders WHERE order_no = $1 LIMIT 1`
	return scanPaymentOrder(r.pool.QueryRow(ctx, query, orderNo))
}

func (r *Repository) MarkPaymentOrderPaid(ctx context.Context, orderID int64, providerOrderNo string, tradeStatus string, rawCallback map[string]any) error {
	raw, _ := json.Marshal(rawCallback)
	_, err := r.pool.Exec(ctx, `UPDATE payment_orders SET provider_order_no = $2, status = 'paid', notify_status = $3, raw_callback = $4, paid_at = COALESCE(paid_at, NOW()), updated_at = NOW() WHERE id = $1`, orderID, nullableString(providerOrderNo), tradeStatus, raw)
	return err
}

func (r *Repository) MarkPaymentOrderCallbackFailed(ctx context.Context, orderID int64, tradeStatus string, rawCallback map[string]any) error {
	raw, _ := json.Marshal(rawCallback)
	_, err := r.pool.Exec(ctx, `UPDATE payment_orders SET notify_status = $2, raw_callback = $3, updated_at = NOW() WHERE id = $1`, orderID, tradeStatus, raw)
	return err
}

func (r *Repository) CreatePaymentCallbackLog(ctx context.Context, appID int64, orderID *int64, paymentMethod string, callbackMethod string, clientIP string, callbackData map[string]any, verificationStatus string, message string) error {
	data, _ := json.Marshal(callbackData)
	_, err := r.pool.Exec(ctx, `INSERT INTO payment_callback_logs (appid, order_id, payment_method, callback_method, client_ip, callback_data, verification_status, message, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())`, appID, orderID, paymentMethod, callbackMethod, nullableString(clientIP), data, verificationStatus, nullableString(message))
	return err
}

func scanPaymentConfig(row interface{ Scan(dest ...any) error }) (*paymentdomain.Config, error) {
	var item paymentdomain.Config
	var raw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.PaymentMethod, &item.ConfigName, &raw, &item.Enabled, &item.IsDefault, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(raw, &item.ConfigData)
	return &item, nil
}

func scanPaymentOrder(row interface{ Scan(dest ...any) error }) (*paymentdomain.Order, error) {
	var item paymentdomain.Order
	var amount string
	var metadata []byte
	var rawCallback []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.ConfigID, &item.OrderNo, &item.ProviderOrderNo, &item.Subject, &item.Body, &amount, &item.PaymentMethod, &item.ProviderType, &item.Status, &item.NotifyStatus, &item.ClientIP, &item.NotifyURL, &item.ReturnURL, &metadata, &rawCallback, &item.PaidAt, &item.ExpireAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.Amount = decimal.RequireFromString(amount)
	_ = json.Unmarshal(metadata, &item.Metadata)
	_ = json.Unmarshal(rawCallback, &item.RawCallback)
	return &item, nil
}

func generatePaymentOrderNo(appID int64) string {
	return fmt.Sprintf("P%d%s", appID, time.Now().UTC().Format("20060102150405")+randomDigits(6))
}
