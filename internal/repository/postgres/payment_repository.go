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

// GetPaymentConfig 按渠道与配置名定位一条支付配置。
//
// paymentMethod 为空表示**不限渠道**，这是应用级下单唯一能走的形状 ——
// `CreatePaymentOrderRequest` 里没有渠道字段，客户端只能给 config_name 或什么都不给。
// 此前两条分支都无条件拼 `payment_method = $2`，而该列 NOT NULL、存的是
// 'epay'/'alipay' 这类真值，于是空串匹配零行：应用级下单无论配了什么都返回
// 「未找到可用支付配置」(40471)。回调路径（传真实 method）不受影响，所以这个洞
// 只在应用级下单这一条链路上显现。
//
// 不限渠道时 config_name 可能在多个渠道下重名（唯一约束是 appid+method+name 三元组），
// 因此必须有确定的排序，否则选中哪条取决于物理行序。优先可用、其次默认、最后按 id。
func (r *Repository) GetPaymentConfig(ctx context.Context, appID int64, paymentMethod string, configName string) (*paymentdomain.Config, error) {
	query, args := buildPaymentConfigLookup(appID, paymentMethod, configName)
	return scanPaymentConfig(r.pool.QueryRow(ctx, query, args...))
}

// buildPaymentConfigLookup 单独拆出来是为了可测：这个 bug 整个长在「谓词该不该拼」上，
// 而仓储层的测试不连库，只有把构造过程暴露出来才钉得住。
func buildPaymentConfigLookup(appID int64, paymentMethod string, configName string) (string, []any) {
	args := []any{appID}
	query := `SELECT id, appid, payment_method, config_name, COALESCE(config_data, '{}'::jsonb), enabled, is_default, COALESCE(description, ''), created_at, updated_at FROM payment_configs WHERE appid = $1`
	if paymentMethod = strings.TrimSpace(paymentMethod); paymentMethod != "" {
		query += fmt.Sprintf(" AND payment_method = $%d", len(args)+1)
		args = append(args, paymentMethod)
	}
	if configName = strings.TrimSpace(configName); configName != "" {
		// 指名道姓要某条配置时不过滤 enabled：让「这条配置被停用了」以 40471 显性报出来，
		// 而不是悄悄回落到另一条配置上把钱收进别的渠道。
		query += fmt.Sprintf(" AND config_name = $%d", len(args)+1)
		args = append(args, configName)
	} else {
		query += " AND enabled = TRUE"
	}
	return query + " ORDER BY enabled DESC, is_default DESC, id ASC LIMIT 1", args
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
	// id 为 0 表示新建：必须显式取 nextval，不能只写 NULL。
	// 显式写入的 NULL 不会回退到列默认值（DEFAULT nextval 只在该列缺席时生效），
	// 直接 NULLIF($1, 0) 会让新建路径必然撞 NOT NULL（23502）。
	query := `INSERT INTO payment_configs (id, appid, payment_method, config_name, config_data, enabled, is_default, description, created_at, updated_at)
VALUES (COALESCE(NULLIF($1, 0), nextval(pg_get_serial_sequence('payment_configs', 'id'))), $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
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
	query := `INSERT INTO payment_orders (appid, user_id, config_id, order_no, subject, body, amount, payment_method, provider_type, status, notify_status, client_ip, notify_url, return_url, metadata, expire_at, currency, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', 'pending', $10, $11, $12, $13, $14, $15, NOW(), NOW())
RETURNING id, appid, user_id, config_id, order_no, COALESCE(provider_order_no, ''), subject, COALESCE(body, ''), amount, payment_method, provider_type, status, notify_status, COALESCE(client_ip, ''), COALESCE(notify_url, ''), COALESCE(return_url, ''), COALESCE(metadata, '{}'::jsonb), COALESCE(raw_callback, '{}'::jsonb), refunded_amount, refund_status, COALESCE(currency, ''), paid_at, expire_at, created_at, updated_at`
	return scanPaymentOrder(r.pool.QueryRow(ctx, query, item.AppID, item.UserID, item.ConfigID, item.OrderNo, item.Subject, nullableString(item.Body), item.Amount.StringFixed(2), item.PaymentMethod, item.ProviderType, nullableString(item.ClientIP), nullableString(item.NotifyURL), nullableString(item.ReturnURL), meta, item.ExpireAt, strings.ToUpper(strings.TrimSpace(item.Currency))))
}

func (r *Repository) GetPaymentOrderByOrderNo(ctx context.Context, orderNo string) (*paymentdomain.Order, error) {
	query := `SELECT id, appid, user_id, config_id, order_no, COALESCE(provider_order_no, ''), subject, COALESCE(body, ''), amount, payment_method, provider_type, status, notify_status, COALESCE(client_ip, ''), COALESCE(notify_url, ''), COALESCE(return_url, ''), COALESCE(metadata, '{}'::jsonb), COALESCE(raw_callback, '{}'::jsonb), refunded_amount, refund_status, COALESCE(currency, ''), paid_at, expire_at, created_at, updated_at FROM payment_orders WHERE order_no = $1 LIMIT 1`
	return scanPaymentOrder(r.pool.QueryRow(ctx, query, orderNo))
}

func (r *Repository) GetPaymentOrderByOrderNoForUser(ctx context.Context, appID int64, userID int64, orderNo string) (*paymentdomain.Order, error) {
	query := `SELECT id, appid, user_id, config_id, order_no, COALESCE(provider_order_no, ''), subject, COALESCE(body, ''), amount, payment_method, provider_type, status, notify_status, COALESCE(client_ip, ''), COALESCE(notify_url, ''), COALESCE(return_url, ''), COALESCE(metadata, '{}'::jsonb), COALESCE(raw_callback, '{}'::jsonb), refunded_amount, refund_status, COALESCE(currency, ''), paid_at, expire_at, created_at, updated_at FROM payment_orders WHERE appid = $1 AND user_id = $2 AND order_no = $3 LIMIT 1`
	return scanPaymentOrder(r.pool.QueryRow(ctx, query, appID, userID, orderNo))
}

func (r *Repository) ListPaymentOrdersByUser(ctx context.Context, appID int64, userID int64, status string, page int, limit int) ([]paymentdomain.Order, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	args := []any{appID, userID}
	countQuery := `SELECT COUNT(*) FROM payment_orders WHERE appid = $1 AND user_id = $2`
	listQuery := `SELECT id, appid, user_id, config_id, order_no, COALESCE(provider_order_no, ''), subject, COALESCE(body, ''), amount, payment_method, provider_type, status, notify_status, COALESCE(client_ip, ''), COALESCE(notify_url, ''), COALESCE(return_url, ''), COALESCE(metadata, '{}'::jsonb), COALESCE(raw_callback, '{}'::jsonb), refunded_amount, refund_status, COALESCE(currency, ''), paid_at, expire_at, created_at, updated_at FROM payment_orders WHERE appid = $1 AND user_id = $2`
	if status = strings.TrimSpace(status); status != "" {
		args = append(args, status)
		countQuery += fmt.Sprintf(" AND status = $%d", len(args))
		listQuery += fmt.Sprintf(" AND status = $%d", len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	listQuery += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]paymentdomain.Order, 0, limit)
	for rows.Next() {
		item, err := scanPaymentOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// ListPaymentOrdersByApp 管理端按应用分页查询订单（支持状态 / 支付方式 / 订单号关键字 / 用户过滤）
func (r *Repository) ListPaymentOrdersByApp(ctx context.Context, appID int64, status string, method string, keyword string, userID int64, page int, limit int) ([]paymentdomain.Order, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	args := []any{appID}
	where := ` WHERE appid = $1`
	if status = strings.TrimSpace(status); status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if method = strings.TrimSpace(method); method != "" {
		args = append(args, method)
		where += fmt.Sprintf(" AND payment_method = $%d", len(args))
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where += fmt.Sprintf(" AND (order_no ILIKE $%d OR provider_order_no ILIKE $%d OR subject ILIKE $%d)", len(args), len(args), len(args))
	}
	if userID > 0 {
		args = append(args, userID)
		where += fmt.Sprintf(" AND user_id = $%d", len(args))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM payment_orders`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, (page-1)*limit)
	query := `SELECT id, appid, user_id, config_id, order_no, COALESCE(provider_order_no, ''), subject, COALESCE(body, ''), amount, payment_method, provider_type, status, notify_status, COALESCE(client_ip, ''), COALESCE(notify_url, ''), COALESCE(return_url, ''), COALESCE(metadata, '{}'::jsonb), COALESCE(raw_callback, '{}'::jsonb), refunded_amount, refund_status, COALESCE(currency, ''), paid_at, expire_at, created_at, updated_at FROM payment_orders` +
		where + fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]paymentdomain.Order, 0, limit)
	for rows.Next() {
		item, err := scanPaymentOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// GetPaymentOrderFulfillment 查询订单履约状态（履约列未纳入通用 Order 结构，按需读取）
func (r *Repository) GetPaymentOrderFulfillment(ctx context.Context, orderID int64) (status string, fulfilledAt *time.Time, err error) {
	err = r.pool.QueryRow(ctx, `SELECT fulfillment_status, fulfilled_at FROM payment_orders WHERE id = $1`, orderID).
		Scan(&status, &fulfilledAt)
	return status, fulfilledAt, err
}

// ListPaymentCallbackLogsByOrder 查询订单的回调处理日志（按时间倒序）
func (r *Repository) ListPaymentCallbackLogsByOrder(ctx context.Context, appID int64, orderID int64, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, payment_method, callback_method, COALESCE(client_ip, ''), COALESCE(callback_data, '{}'::jsonb), verification_status, COALESCE(message, ''), created_at
FROM payment_callback_logs WHERE appid = $1 AND order_id = $2 ORDER BY id DESC LIMIT $3`,
		appID, orderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var id int64
		var method, callbackMethod, clientIP, verificationStatus, message string
		var data []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &method, &callbackMethod, &clientIP, &data, &verificationStatus, &message, &createdAt); err != nil {
			return nil, err
		}
		var callbackData map[string]any
		_ = json.Unmarshal(data, &callbackData)
		items = append(items, map[string]any{
			"id":                  id,
			"payment_method":      method,
			"callback_method":     callbackMethod,
			"client_ip":           clientIP,
			"callback_data":       callbackData,
			"verification_status": verificationStatus,
			"message":             message,
			"created_at":          createdAt,
		})
	}
	return items, rows.Err()
}

// MarkPaymentOrderPaid 幂等地把订单标记为已支付。
// 返回 firstTime=true 表示本次首次完成支付确认（仅此时应触发履约等一次性副作用）；
// 重复回调命中 status='paid' 守卫后不再改写任何字段。
func (r *Repository) MarkPaymentOrderPaid(ctx context.Context, orderID int64, providerOrderNo string, tradeStatus string, rawCallback map[string]any) (bool, error) {
	raw, _ := json.Marshal(rawCallback)
	result, err := r.pool.Exec(ctx, `UPDATE payment_orders SET provider_order_no = $2, status = 'paid', notify_status = $3, raw_callback = $4, paid_at = COALESCE(paid_at, NOW()), updated_at = NOW() WHERE id = $1 AND status <> 'paid'`, orderID, nullableString(providerOrderNo), tradeStatus, raw)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
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
	var refundedAmount string
	var metadata []byte
	var rawCallback []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.ConfigID, &item.OrderNo, &item.ProviderOrderNo, &item.Subject, &item.Body, &amount, &item.PaymentMethod, &item.ProviderType, &item.Status, &item.NotifyStatus, &item.ClientIP, &item.NotifyURL, &item.ReturnURL, &metadata, &rawCallback, &refundedAmount, &item.RefundStatus, &item.Currency, &item.PaidAt, &item.ExpireAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.Amount = decimal.RequireFromString(amount)
	item.RefundedAmount = decimal.RequireFromString(refundedAmount)
	_ = json.Unmarshal(metadata, &item.Metadata)
	_ = json.Unmarshal(rawCallback, &item.RawCallback)
	return &item, nil
}

func generatePaymentOrderNo(appID int64) string {
	return fmt.Sprintf("P%d%s", appID, time.Now().UTC().Format("20060102150405")+randomDigits(6))
}

// timeWindowClause 拼一段 `列 BETWEEN` 条件。
// 列名由调用方给，因为每种口径该按哪一列筛是不一样的（见 PaymentOrderStats）。
func timeWindowClause(column string, args []any, start *time.Time, end *time.Time) (string, []any) {
	clause := ""
	if start != nil && !start.IsZero() {
		args = append(args, *start)
		clause += fmt.Sprintf(" AND %s >= $%d", column, len(args))
	}
	if end != nil && !end.IsZero() {
		args = append(args, *end)
		clause += fmt.Sprintf(" AND %s <= $%d", column, len(args))
	}
	return clause, args
}

// PaymentOrderStats 应用维度的订单资金面板。
//
// 订单与退款分两条语句而不是一次左连接：一张订单上可以有多笔退款，
// 连起来会把订单金额按退款笔数重复累加 —— 那是最典型的「报表比实收多一倍」。
//
// **三种口径各按自己的时间列筛**，与 PaymentTrend 保持一致：
//
//	| 指标 | 时间列 | 为什么 |
//	|---|---|---|
//	| 已支付金额 / 笔数 / 付费用户 | `paid_at` | 钱是什么时候到的 |
//	| 订单总数 / 待支付 | `created_at` | 待支付的单没有 paid_at，只能按创建时刻 |
//	| 已退款 | `refunded_at` | 退款成功的时刻才是钱出去的时刻 |
//
// 全部按 created_at 会让「月末下单、次月初付款」的收入落在错误的月份上。
func (r *Repository) PaymentOrderStats(ctx context.Context, appID int64, start *time.Time, end *time.Time) (*paymentdomain.OrderStats, error) {
	stats := &paymentdomain.OrderStats{
		ByStatus: make([]paymentdomain.OrderGroupStat, 0, 6),
		ByMethod: make([]paymentdomain.OrderGroupStat, 0, 8),
	}

	// 已支付口径：按到账时刻
	paidWindow, paidArgs := timeWindowClause("paid_at", []any{appID}, start, end)
	paidWhere := ` WHERE appid = $1 AND status = 'paid' AND paid_at IS NOT NULL` + paidWindow
	var paidAmount string
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(SUM(amount), 0)::text, COUNT(DISTINCT user_id)
FROM payment_orders`+paidWhere, paidArgs...).
		Scan(&stats.PaidOrders, &paidAmount, &stats.PayerCount); err != nil {
		return nil, err
	}
	stats.PaidAmount = decimal.RequireFromString(paidAmount)

	// 下单口径：按创建时刻
	createdWindow, createdArgs := timeWindowClause("created_at", []any{appID}, start, end)
	createdWhere := ` WHERE appid = $1` + createdWindow
	var pendingAmount string
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'pending'),
COALESCE(SUM(amount) FILTER (WHERE status = 'pending'), 0)::text
FROM payment_orders`+createdWhere, createdArgs...).
		Scan(&stats.TotalOrders, &stats.PendingOrders, &pendingAmount); err != nil {
		return nil, err
	}
	stats.PendingAmount = decimal.RequireFromString(pendingAmount)

	// 分布按下单口径：它回答的是「这批订单长什么样」，含未支付的那些
	if err := scanOrderGroupStats(ctx, r, &stats.ByStatus,
		`SELECT status, COUNT(*), COALESCE(SUM(amount), 0)::text FROM payment_orders`+createdWhere+
			` GROUP BY status ORDER BY status`, createdArgs); err != nil {
		return nil, err
	}
	if err := scanOrderGroupStats(ctx, r, &stats.ByMethod,
		`SELECT payment_method, COUNT(*), COALESCE(SUM(amount), 0)::text FROM payment_orders`+createdWhere+
			` GROUP BY payment_method ORDER BY payment_method`, createdArgs); err != nil {
		return nil, err
	}

	// 退款只统计**已成功**的：预占额度里含在途退款，把它算进「已退」
	// 会让实收看起来比银行流水少一截。
	refundWindow, refundArgs := timeWindowClause("COALESCE(refunded_at, updated_at)", []any{appID}, start, end)
	var refundedAmount string
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(SUM(amount), 0)::text FROM payment_refunds
WHERE appid = $1 AND status = 'success'`+refundWindow, refundArgs...).
		Scan(&stats.RefundCount, &refundedAmount); err != nil {
		return nil, err
	}
	stats.RefundedAmount = decimal.RequireFromString(refundedAmount)
	stats.NetAmount = stats.PaidAmount.Sub(stats.RefundedAmount)
	return stats, nil
}

func scanOrderGroupStats(ctx context.Context, r *Repository, out *[]paymentdomain.OrderGroupStat, query string, args []any) error {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item paymentdomain.OrderGroupStat
		var amount string
		if err := rows.Scan(&item.Key, &item.Count, &amount); err != nil {
			return err
		}
		item.Amount = decimal.RequireFromString(amount)
		*out = append(*out, item)
	}
	return rows.Err()
}

// PaymentTrend 应用维度的交易趋势。
//
// 三条线放在同一组时间桶里（实收 / 退款 / 钱包出入），因为它们只有放在一起才有意义：
// 「这个月收了 10 万」单独看说明不了什么，要和「退了 3 万、钱包又花掉 2 万」一起看。
//
// **每条线按自己的时间列分桶**，这是刻意的：
//   - 实收按 `paid_at` —— 1 号下单、5 号付款的那笔钱是 5 号到的账；
//   - 退款按 `refunded_at` —— 退款成功的时刻才是钱出去的时刻；
//   - 钱包按 `created_at` —— 流水写进表的那一刻钱就已经动了，没有第二个时刻。
//
// 用 created_at 统一分桶会让「月末下单、次月初付款」的收入落在错误的月份上，
// 而那正是对账时最容易被质疑的一格。
func (r *Repository) PaymentTrend(ctx context.Context, appID int64, start *time.Time, end *time.Time) (*paymentdomain.Trend, error) {
	from, to, ok, err := r.resolveTrendWindow(ctx, appID, start, end)
	if err != nil {
		return nil, err
	}
	trend := &paymentdomain.Trend{Bucket: paymentdomain.TrendBucketDay, Points: []paymentdomain.TrendPoint{}}
	if !ok {
		// 该应用还没有任何资金记录：返回空序列而不是造一串零点，
		// 「没有数据」与「每天都是 0」在图上应当长得不一样。
		return trend, nil
	}
	unit := trendBucketUnit(from, to)
	trend.Bucket = unit

	points := map[time.Time]*paymentdomain.TrendPoint{}
	at := func(bucket time.Time) *paymentdomain.TrendPoint {
		if existing, hit := points[bucket]; hit {
			return existing
		}
		point := &paymentdomain.TrendPoint{
			Bucket: bucket, Label: trendLabel(unit, bucket),
			PaidAmount: decimal.Zero, RefundedAmount: decimal.Zero, NetAmount: decimal.Zero,
			WalletIn: decimal.Zero, WalletOut: decimal.Zero,
		}
		points[bucket] = point
		return point
	}

	// 实收
	if err := r.scanTrendRows(ctx,
		`SELECT date_trunc($2::text, paid_at AT TIME ZONE 'UTC') AS bucket,
COALESCE(SUM(amount), 0)::text, COUNT(*)
FROM payment_orders
WHERE appid = $1 AND status = 'paid' AND paid_at IS NOT NULL AND paid_at >= $3 AND paid_at <= $4
GROUP BY bucket`,
		[]any{appID, unit, from, to},
		func(bucket time.Time, amount decimal.Decimal, count int64) {
			point := at(bucket)
			point.PaidAmount = point.PaidAmount.Add(amount)
			point.PaidOrders += count
		}); err != nil {
		return nil, err
	}

	// 退款（只算已成功的；预占额度里含在途退款）
	if err := r.scanTrendRows(ctx,
		`SELECT date_trunc($2::text, COALESCE(refunded_at, updated_at) AT TIME ZONE 'UTC') AS bucket,
COALESCE(SUM(amount), 0)::text, COUNT(*)
FROM payment_refunds
WHERE appid = $1 AND status = 'success'
  AND COALESCE(refunded_at, updated_at) >= $3 AND COALESCE(refunded_at, updated_at) <= $4
GROUP BY bucket`,
		[]any{appID, unit, from, to},
		func(bucket time.Time, amount decimal.Decimal, _ int64) {
			point := at(bucket)
			point.RefundedAmount = point.RefundedAmount.Add(amount)
		}); err != nil {
		return nil, err
	}

	// 钱包出入（一条语句两个方向，避免为同一张表扫两遍）
	rows, err := r.pool.Query(ctx,
		`SELECT date_trunc($2::text, created_at AT TIME ZONE 'UTC') AS bucket,
COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0)::text,
COALESCE(-SUM(amount) FILTER (WHERE amount < 0), 0)::text
FROM wallet_transactions
WHERE appid = $1 AND created_at >= $3 AND created_at <= $4
GROUP BY bucket`,
		appID, unit, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var bucket time.Time
		var in, out string
		if err := rows.Scan(&bucket, &in, &out); err != nil {
			return nil, err
		}
		point := at(bucket.UTC())
		point.WalletIn = decimal.RequireFromString(in)
		point.WalletOut = decimal.RequireFromString(out)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 补齐空桶：缺口在折线图上会被连成一条斜线，读起来像「那几天缓慢下降」，
	// 而事实是那几天一笔都没有。
	for cursor := truncateToBucket(unit, from); !cursor.After(to); cursor = stepBucket(unit, cursor) {
		point := at(cursor)
		point.NetAmount = point.PaidAmount.Sub(point.RefundedAmount)
		trend.Points = append(trend.Points, *point)
	}
	return trend, nil
}

// resolveTrendWindow 定出趋势的时间窗。
// 调用方没给边界时用数据自身的最早 / 最晚时刻兜底 —— 否则「全部」这档要么画不出来，
// 要么得从应用创建那天开始画一串空桶。
func (r *Repository) resolveTrendWindow(ctx context.Context, appID int64, start *time.Time, end *time.Time) (time.Time, time.Time, bool, error) {
	from, to := time.Time{}, time.Time{}
	if start != nil && !start.IsZero() {
		from = start.UTC()
	}
	if end != nil && !end.IsZero() {
		to = end.UTC()
	}
	if !from.IsZero() && !to.IsZero() {
		if to.Before(from) {
			from, to = to, from
		}
		return from, to, true, nil
	}

	var minAt, maxAt *time.Time
	if err := r.pool.QueryRow(ctx,
		`SELECT MIN(ts), MAX(ts) FROM (
    SELECT paid_at AS ts FROM payment_orders WHERE appid = $1 AND paid_at IS NOT NULL
    UNION ALL
    SELECT created_at FROM wallet_transactions WHERE appid = $1
) AS all_ts`, appID).Scan(&minAt, &maxAt); err != nil {
		return from, to, false, err
	}
	if minAt == nil || maxAt == nil {
		return from, to, false, nil
	}
	if from.IsZero() {
		from = minAt.UTC()
	}
	if to.IsZero() {
		to = maxAt.UTC()
	}
	if to.Before(from) {
		from, to = to, from
	}
	return from, to, true, nil
}

func (r *Repository) scanTrendRows(ctx context.Context, query string, args []any, apply func(bucket time.Time, amount decimal.Decimal, count int64)) error {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var bucket time.Time
		var amount string
		var count int64
		if err := rows.Scan(&bucket, &amount, &count); err != nil {
			return err
		}
		apply(bucket.UTC(), decimal.RequireFromString(amount), count)
	}
	return rows.Err()
}

// trendBucketUnit 按跨度自动选粒度，不让调用方指定 ——
// 让前端选粒度的结果是「拉了两年、按天分桶、七百个点」这种没人看得懂的图。
func trendBucketUnit(from time.Time, to time.Time) string {
	days := int(to.Sub(from).Hours() / 24)
	switch {
	case days <= 62:
		return paymentdomain.TrendBucketDay
	case days <= 730:
		return paymentdomain.TrendBucketWeek
	default:
		return paymentdomain.TrendBucketMonth
	}
}

// truncateToBucket 与 Postgres 的 date_trunc 对齐（周起于周一），
// 否则补出来的空桶落不到真实数据那一格上，同一天会出现两个点。
func truncateToBucket(unit string, at time.Time) time.Time {
	at = at.UTC()
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	switch unit {
	case paymentdomain.TrendBucketMonth:
		return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	case paymentdomain.TrendBucketWeek:
		offset := (int(day.Weekday()) + 6) % 7 // 周一为 0
		return day.AddDate(0, 0, -offset)
	default:
		return day
	}
}

func stepBucket(unit string, at time.Time) time.Time {
	switch unit {
	case paymentdomain.TrendBucketMonth:
		return at.AddDate(0, 1, 0)
	case paymentdomain.TrendBucketWeek:
		return at.AddDate(0, 0, 7)
	default:
		return at.AddDate(0, 0, 1)
	}
}

// trendLabel 直接可展示的短标签。在这里生成而不是让前端格式化：
// 桶的粒度是服务端定的，前端拿不到粒度就只能猜该显示到哪一位。
func trendLabel(unit string, at time.Time) string {
	switch unit {
	case paymentdomain.TrendBucketMonth:
		return at.Format("2006-01")
	default:
		return at.Format("01-02")
	}
}
