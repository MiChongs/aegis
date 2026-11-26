package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"aegis/internal/domain/aigateway"
	walletdomain "aegis/internal/domain/wallet"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

// AI 额度商店与额度批次（docs/ai-billing.md）：额度包、订单、批次、新人礼、到期清理、经营统计。
//
// 锁序与计费事务一致：users → ai_gateway_accounts → ai_gateway_packages → ai_gateway_credit_lots → user_wallets。
// 凡是动批次的事务都先锁 users 行，与暂扣 / 结算彼此串行。

var (
	ErrAIGatewayPackageNotFound   = errors.New("ai gateway: package not found")
	ErrAIGatewayPackageInUse      = errors.New("ai gateway: package has orders")
	ErrAIGatewayPriceChanged      = errors.New("ai gateway: package price changed")
	ErrAIGatewayWalletShort       = errors.New("ai gateway: wallet balance insufficient")
	ErrAIGatewayOrderNotFound     = errors.New("ai gateway: order not found")
	ErrAIGatewayOrderNotRefund    = errors.New("ai gateway: order not refundable")
	ErrAIGatewayOrderLotBusy      = errors.New("ai gateway: order lot is held by a request")
	ErrAIGatewayWelcomeClaimed    = errors.New("ai gateway: welcome gift not available")
	ErrAIGatewayLotNotFound       = errors.New("ai gateway: lot not found")
	ErrAIGatewayRequestIDReused   = errors.New("ai gateway: request id reused")
	ErrAIGatewayLotUnavailable    = errors.New("ai gateway: lot already ended")
)

// AIGatewayUnavailableError 额度包此刻不能买（Reason 见 aigateway.Reason*）。
type AIGatewayUnavailableError struct{ Reason string }

func (e *AIGatewayUnavailableError) Error() string { return "ai gateway: package unavailable: " + e.Reason }

// ── 批次的底层操作 ──

// aiGatewayLot 暂扣 / 结算时锁住的一个可用批次。
type aiGatewayLot struct {
	id        int64
	remaining int64
}

// lockUsableAIGatewayLotsTx 锁住用户此刻可用的批次：先到期的在前，永久的最后，同时到期按 id。
func lockUsableAIGatewayLotsTx(ctx context.Context, tx pgx.Tx, appID, userID int64, now time.Time) ([]aiGatewayLot, error) {
	rows, err := tx.Query(ctx, `SELECT id, remaining FROM ai_gateway_credit_lots
WHERE user_id = $1 AND appid = $2 AND status = 'active' AND remaining > 0 AND (expires_at IS NULL OR expires_at > $3)
ORDER BY expires_at ASC NULLS LAST, id ASC FOR UPDATE`, userID, appID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []aiGatewayLot{}
	for rows.Next() {
		var lot aiGatewayLot
		if err := rows.Scan(&lot.id, &lot.remaining); err != nil {
			return nil, err
		}
		out = append(out, lot)
	}
	return out, rows.Err()
}

// adjustAIGatewayLotTx 调整批次的可用与暂扣。CHECK 约束保证 remaining + frozen ≤ amount、都不为负。
func adjustAIGatewayLotTx(ctx context.Context, tx pgx.Tx, lotID, remainingDelta, frozenDelta int64) error {
	if remainingDelta == 0 && frozenDelta == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `UPDATE ai_gateway_credit_lots SET remaining = remaining + $2, frozen = frozen + $3, updated_at = NOW()
WHERE id = $1`, lotID, remainingDelta, frozenDelta)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("ai gateway: lot %d missing", lotID)
	}
	return nil
}

// restoreAIGatewayLotTx 调用退款时把 micro 退回批次：批次还有效时最多退回它被用掉的部分，返回退回了多少（余下的由调用方退为永久额度）。
func restoreAIGatewayLotTx(ctx context.Context, tx pgx.Tx, lotID, micro int64, now time.Time) (int64, error) {
	if micro <= 0 {
		return 0, nil
	}
	var amount, remaining, frozen int64
	var status string
	var expiresAt *time.Time
	err := tx.QueryRow(ctx, `SELECT amount, remaining, frozen, status, expires_at FROM ai_gateway_credit_lots WHERE id = $1 FOR UPDATE`, lotID).
		Scan(&amount, &remaining, &frozen, &status, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !aigateway.LotUsable(status, expiresAt, now) {
		return 0, nil
	}
	back := min(micro, max(amount-remaining-frozen, 0))
	if back <= 0 {
		return 0, nil
	}
	return back, adjustAIGatewayLotTx(ctx, tx, lotID, back, 0)
}

// aiGatewayPackTotalTx 用户此刻可用的额度包合计（不含暂扣中）。
func aiGatewayPackTotalTx(ctx context.Context, q queryExecutor, appID, userID int64, now time.Time) (int64, error) {
	var total int64
	err := q.QueryRow(ctx, `SELECT COALESCE(SUM(remaining), 0)::bigint FROM ai_gateway_credit_lots
WHERE user_id = $1 AND appid = $2 AND status = 'active' AND (expires_at IS NULL OR expires_at > $3)`, userID, appID, now).Scan(&total)
	return total, err
}

// aiGatewayProfileQ 计价画像：会员、等级、此刻持有的档位（与会员判定同一份段）。
func aiGatewayProfileQ(ctx context.Context, q queryExecutor, appID, userID int64, isVip bool) (aigateway.Profile, error) {
	profile := aigateway.Profile{IsVip: isVip, Level: 1}
	if err := q.QueryRow(ctx, `SELECT COALESCE((SELECT current_level FROM user_level_records WHERE user_id = $1 AND appid = $2), 1)`,
		userID, appID).Scan(&profile.Level); err != nil {
		return profile, err
	}
	if !isVip {
		return profile, nil
	}
	holdings, err := vipHoldingsQ(ctx, q, appID, userID)
	if err != nil {
		return profile, err
	}
	names := map[int64]string{}
	for _, tier := range holdings.Tiers {
		profile.TierIDs = append(profile.TierIDs, tier.ID)
		names[tier.ID] = tier.Name
	}
	return profile.WithTierNames(names), nil
}

// grantAIGatewayLotTx 建一个额度批次并写流水。调用方已锁住 users 与 ai_gateway_accounts（ledger）。
func grantAIGatewayLotTx(ctx context.Context, ledger *aiGatewayLedger, in aigateway.LotGrantInput, orderNo string, packageID int64,
	kind string, now time.Time) (*aigateway.CreditLot, error) {
	micro := aigateway.PointsToMicroFloor(in.Amount)
	if micro <= 0 {
		return nil, errors.New("ai gateway: lot amount must be positive")
	}
	expiresAt := aigateway.LotExpiry(now, in.ValidityDays)
	var pkg *int64
	if packageID > 0 {
		pkg = &packageID
	}
	var id int64
	if err := ledger.tx.QueryRow(ctx, `INSERT INTO ai_gateway_credit_lots
(appid, user_id, origin, title, amount, remaining, order_no, package_id, expires_at, request_id, operator, remark, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8, $9, $10, $11, $12, $12) RETURNING id`,
		in.AppID, in.UserID, in.Origin, truncateColumn(in.Title, 64), micro, nullableString(orderNo), pkg, expiresAt,
		nullableString(in.RequestID), nullableString(in.Operator), truncateColumn(in.Remark, 200), now).Scan(&id); err != nil {
		return nil, err
	}
	refType, refID := "lot", fmt.Sprint(id)
	if orderNo != "" {
		refType, refID = "package_order", orderNo
	}
	remark := in.Remark
	if remark == "" {
		remark = in.Title
	}
	if err := ledger.insertLedger(ctx, kind, aigateway.BucketPack, micro, id, refType, refID, remark, in.Operator, ""); err != nil {
		return nil, err
	}
	return getAIGatewayLotQ(ctx, ledger.tx, id, false)
}

const aiGatewayLotColumns = `l.id, l.origin, l.title, l.amount, l.remaining, l.frozen, COALESCE(l.order_no, ''), COALESCE(l.package_id, 0),
l.expires_at, l.status, l.created_at, l.user_id, COALESCE(u.account, '')`

func scanAIGatewayLot(row interface{ Scan(dest ...any) error }) (*aigateway.CreditLot, error) {
	var lot aigateway.CreditLot
	if err := row.Scan(&lot.ID, &lot.Origin, &lot.Title, &lot.AmountMicro, &lot.RemainingMicro, &lot.FrozenMicro, &lot.OrderNo,
		&lot.PackageID, &lot.ExpiresAt, &lot.Status, &lot.CreatedAt, &lot.UserID, &lot.Account); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAIGatewayLotNotFound
		}
		return nil, err
	}
	lot.Finish()
	return &lot, nil
}

func getAIGatewayLotQ(ctx context.Context, q queryExecutor, id int64, forUpdate bool) (*aigateway.CreditLot, error) {
	query := `SELECT ` + aiGatewayLotColumns + ` FROM ai_gateway_credit_lots l LEFT JOIN users u ON u.id = l.user_id WHERE l.id = $1`
	if forUpdate {
		query += ` FOR UPDATE OF l`
	}
	return scanAIGatewayLot(q.QueryRow(ctx, query, id))
}

// ── 额度包 ──

const aiGatewayPackageColumns = `p.id, p.appid, p.name, p.subtitle, p.description, p.badge, p.tone, p.points, p.bonus_points,
p.price::text, p.original_price::text, p.validity_days, p.audience, p.tier_ids, p.member_discount, p.per_user_limit,
p.monthly_limit, p.stock, p.sold, p.starts_at, p.ends_at, p.featured, p.enabled, p.sort_order, p.created_at, p.updated_at`

func scanAIGatewayPackage(row interface{ Scan(dest ...any) error }, extra ...any) (*aigateway.Package, error) {
	var pkg aigateway.Package
	var points, bonus int64
	var price, original string
	dest := []any{&pkg.ID, &pkg.AppID, &pkg.Name, &pkg.Subtitle, &pkg.Description, &pkg.Badge, &pkg.Tone, &points, &bonus,
		&price, &original, &pkg.ValidityDays, &pkg.Audience, &pkg.TierIDs, &pkg.MemberDiscount, &pkg.PerUserLimit,
		&pkg.MonthlyLimit, &pkg.Stock, &pkg.Sold, &pkg.StartsAt, &pkg.EndsAt, &pkg.Featured, &pkg.Enabled, &pkg.SortOrder,
		&pkg.CreatedAt, &pkg.UpdatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAIGatewayPackageNotFound
		}
		return nil, err
	}
	pkg.Points = aigateway.MicroToPoints(points)
	pkg.BonusPoints = aigateway.MicroToPoints(bonus)
	pkg.Price = decimal.RequireFromString(price)
	pkg.OriginalPrice = decimal.RequireFromString(original)
	if pkg.TierIDs == nil {
		pkg.TierIDs = []int64{}
	}
	return &pkg, nil
}

// ListAIGatewayPackages 应用的额度包；withStats 时带订单数、买家数与营收（管理端）。
func (r *Repository) ListAIGatewayPackages(ctx context.Context, appID int64, enabledOnly, withStats bool) ([]aigateway.Package, error) {
	query := `SELECT ` + aiGatewayPackageColumns
	if withStats {
		query += `, COALESCE(s.orders, 0), COALESCE(s.buyers, 0), COALESCE(s.revenue, 0)::text`
	}
	query += ` FROM ai_gateway_packages p`
	if withStats {
		query += ` LEFT JOIN LATERAL (SELECT COUNT(*) FILTER (WHERE o.status = 'paid') AS orders,
  COUNT(DISTINCT o.user_id) FILTER (WHERE o.status = 'paid') AS buyers,
  SUM(o.price - o.refund_amount) AS revenue
  FROM ai_gateway_package_orders o WHERE o.appid = p.appid AND o.package_id = p.id) s ON TRUE`
	}
	query += ` WHERE p.appid = $1`
	if enabledOnly {
		query += ` AND p.enabled`
	}
	query += ` ORDER BY p.sort_order ASC, p.id ASC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []aigateway.Package{}
	for rows.Next() {
		var orders, buyers int64
		var revenue string
		extra := []any{}
		if withStats {
			extra = []any{&orders, &buyers, &revenue}
		}
		pkg, err := scanAIGatewayPackage(rows, extra...)
		if err != nil {
			return nil, err
		}
		if withStats {
			pkg.Orders, pkg.Buyers = orders, buyers
			value := decimal.RequireFromString(revenue)
			pkg.Revenue = &value
		}
		out = append(out, *pkg)
	}
	return out, rows.Err()
}

// GetAIGatewayPackage 一个额度包。
func (r *Repository) GetAIGatewayPackage(ctx context.Context, appID, id int64) (*aigateway.Package, error) {
	return scanAIGatewayPackage(r.pool.QueryRow(ctx, `SELECT `+aiGatewayPackageColumns+` FROM ai_gateway_packages p
WHERE p.appid = $1 AND p.id = $2`, appID, id))
}

func packageArgs(pkg aigateway.Package) []any {
	return []any{pkg.Name, pkg.Subtitle, pkg.Description, pkg.Badge, pkg.Tone, aigateway.PointsToMicroFloor(pkg.Points),
		aigateway.PointsToMicroFloor(pkg.BonusPoints), pkg.Price.StringFixed(2), pkg.OriginalPrice.StringFixed(2),
		pkg.ValidityDays, pkg.Audience, pkg.TierIDs, pkg.MemberDiscount, pkg.PerUserLimit, pkg.MonthlyLimit, pkg.Stock,
		pkg.StartsAt, pkg.EndsAt, pkg.Featured, pkg.Enabled, pkg.SortOrder}
}

// CreateAIGatewayPackage 新建额度包。
func (r *Repository) CreateAIGatewayPackage(ctx context.Context, appID int64, pkg aigateway.Package) (*aigateway.Package, error) {
	var id int64
	args := append([]any{appID}, packageArgs(pkg)...)
	if err := r.pool.QueryRow(ctx, `INSERT INTO ai_gateway_packages
(appid, name, subtitle, description, badge, tone, points, bonus_points, price, original_price, validity_days, audience,
 tier_ids, member_discount, per_user_limit, monthly_limit, stock, starts_at, ends_at, featured, enabled, sort_order)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::numeric,$10::numeric,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22) RETURNING id`, args...).Scan(&id); err != nil {
		return nil, err
	}
	return r.GetAIGatewayPackage(ctx, appID, id)
}

// UpdateAIGatewayPackage 修改额度包（整体替换，已售数不动）。已买到的批次与订单按下单时的快照，不受影响。
func (r *Repository) UpdateAIGatewayPackage(ctx context.Context, appID, id int64, pkg aigateway.Package) (*aigateway.Package, error) {
	args := append([]any{appID, id}, packageArgs(pkg)...)
	tag, err := r.pool.Exec(ctx, `UPDATE ai_gateway_packages SET name = $3, subtitle = $4, description = $5, badge = $6, tone = $7,
points = $8, bonus_points = $9, price = $10::numeric, original_price = $11::numeric, validity_days = $12, audience = $13,
tier_ids = $14, member_discount = $15, per_user_limit = $16, monthly_limit = $17, stock = $18, starts_at = $19, ends_at = $20,
featured = $21, enabled = $22, sort_order = $23, updated_at = NOW()
WHERE appid = $1 AND id = $2`, args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAIGatewayPackageNotFound
	}
	return r.GetAIGatewayPackage(ctx, appID, id)
}

// SetAIGatewayPackageEnabled 上架 / 下架。
func (r *Repository) SetAIGatewayPackageEnabled(ctx context.Context, appID, id int64, enabled bool) (*aigateway.Package, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE ai_gateway_packages SET enabled = $3, updated_at = NOW() WHERE appid = $1 AND id = $2`,
		appID, id, enabled)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAIGatewayPackageNotFound
	}
	return r.GetAIGatewayPackage(ctx, appID, id)
}

// DeleteAIGatewayPackage 删除没有订单的额度包；有订单的只能下架（订单要能对上包）。
func (r *Repository) DeleteAIGatewayPackage(ctx context.Context, appID, id int64) error {
	var orders int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_package_orders WHERE appid = $1 AND package_id = $2`,
		appID, id).Scan(&orders); err != nil {
		return err
	}
	if orders > 0 {
		return ErrAIGatewayPackageInUse
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM ai_gateway_packages WHERE appid = $1 AND id = $2`, appID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAIGatewayPackageNotFound
	}
	return nil
}

// AIGatewayPurchaseCounts 某人各额度包的有效订单数：累计与本自然月（按设置时区）。
func (r *Repository) AIGatewayPurchaseCounts(ctx context.Context, appID, userID int64, monthStart time.Time) (map[int64][2]int, error) {
	return aiGatewayPurchaseCountsQ(ctx, r.pool, appID, userID, 0, monthStart)
}

func aiGatewayPurchaseCountsQ(ctx context.Context, q queryExecutor, appID, userID, packageID int64, monthStart time.Time) (map[int64][2]int, error) {
	query := `SELECT package_id, COUNT(*), COUNT(*) FILTER (WHERE created_at >= $3) FROM ai_gateway_package_orders
WHERE appid = $1 AND user_id = $2 AND status = 'paid'`
	args := []any{appID, userID, monthStart}
	if packageID > 0 {
		query += ` AND package_id = $4`
		args = append(args, packageID)
	}
	rows, err := q.Query(ctx, query+` GROUP BY package_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][2]int{}
	for rows.Next() {
		var id int64
		var total, month int
		if err := rows.Scan(&id, &total, &month); err != nil {
			return nil, err
		}
		out[id] = [2]int{total, month}
	}
	return out, rows.Err()
}

// MonthStart 本自然月在 loc 下的起点。
func MonthStart(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
}

// ── 订单 ──

const aiGatewayOrderColumns = `o.order_no, o.package_id, o.package_name, o.list_price::text, o.price::text, o.discount_label,
o.points, o.bonus_points, o.validity_days, o.expires_at, COALESCE(o.lot_id, 0), COALESCE(o.wallet_txn_no, ''), o.status,
o.refund_amount::text, o.refund_points, COALESCE(o.refund_reason, ''), o.refunded_at, o.created_at, o.user_id,
COALESCE(u.account, ''), COALESCE(o.refunded_by, ''), o.request_id,
lot.amount, CASE WHEN lot.status = 'active' AND (lot.expires_at IS NULL OR lot.expires_at > NOW()) THEN lot.remaining ELSE 0 END,
lot.frozen`

const aiGatewayOrderJoins = ` FROM ai_gateway_package_orders o LEFT JOIN users u ON u.id = o.user_id
LEFT JOIN ai_gateway_credit_lots lot ON lot.id = o.lot_id`

func scanAIGatewayOrder(row interface{ Scan(dest ...any) error }) (*aigateway.PackageOrder, string, error) {
	var order aigateway.PackageOrder
	var listPrice, price, refund, requestID string
	var points, bonus, refundPoints int64
	var lotAmount, lotRemaining, lotFrozen *int64
	if err := row.Scan(&order.OrderNo, &order.PackageID, &order.PackageName, &listPrice, &price, &order.DiscountLabel, &points,
		&bonus, &order.ValidityDays, &order.ExpiresAt, &order.LotID, &order.WalletTxnNo, &order.Status, &refund, &refundPoints,
		&order.RefundReason, &order.RefundedAt, &order.CreatedAt, &order.UserID, &order.Account, &order.RefundedBy,
		&requestID, &lotAmount, &lotRemaining, &lotFrozen); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrAIGatewayOrderNotFound
		}
		return nil, "", err
	}
	order.ListPrice = decimal.RequireFromString(listPrice)
	order.Price = decimal.RequireFromString(price)
	order.Discount = order.ListPrice.Sub(order.Price)
	order.Points = aigateway.MicroToPoints(points)
	order.BonusPoints = aigateway.MicroToPoints(bonus)
	order.TotalPoints = aigateway.MicroToPoints(points + bonus)
	order.RefundAmount = decimal.RequireFromString(refund)
	order.RefundPoints = aigateway.MicroToPoints(refundPoints)
	if order.Status == aigateway.OrderPaid && lotAmount != nil && lotRemaining != nil && lotFrozen != nil {
		amount, remaining, frozen := aigateway.MicroToPoints(*lotAmount), aigateway.MicroToPoints(*lotRemaining), aigateway.MicroToPoints(*lotFrozen)
		estimate := aigateway.RefundByRemaining(order.Price, *lotRemaining, *lotAmount)
		order.LotAmount, order.LotRemaining, order.LotFrozen, order.RefundEstimate = &amount, &remaining, &frozen, &estimate
	}
	return &order, requestID, nil
}

// userFacing 去掉只给管理端看的字段。
func userFacingOrder(order *aigateway.PackageOrder) {
	order.UserID, order.Account, order.RefundedBy = 0, "", ""
	order.LotAmount, order.LotRemaining, order.LotFrozen, order.RefundEstimate = nil, nil, nil, nil
}

func getAIGatewayOrderQ(ctx context.Context, q queryExecutor, where string, args ...any) (*aigateway.PackageOrder, string, error) {
	return scanAIGatewayOrder(q.QueryRow(ctx, `SELECT `+aiGatewayOrderColumns+aiGatewayOrderJoins+` WHERE `+where, args...))
}

// ListAIGatewayOrders 订单列表。
func (r *Repository) ListAIGatewayOrders(ctx context.Context, query aigateway.OrderQuery) (*aigateway.Page[aigateway.PackageOrder], error) {
	page, limit := aiGatewayPageLimit(query.Page, query.Limit, 100)
	where := []string{"o.appid = $1"}
	args := []any{query.AppID}
	add := func(cond string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if query.UserID > 0 {
		add("o.user_id = $%d", query.UserID)
	}
	if query.PackageID > 0 {
		add("o.package_id = $%d", query.PackageID)
	}
	if query.Status != "" {
		add("o.status = $%d", query.Status)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		add("o.order_no LIKE $%d", escapeLike(keyword)+"%")
	}
	if query.From != nil {
		add("o.created_at >= $%d", *query.From)
	}
	if query.To != nil {
		add("o.created_at < $%d", *query.To)
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_package_orders o WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+aiGatewayOrderColumns+aiGatewayOrderJoins+` WHERE `+clause+fmt.Sprintf(` ORDER BY o.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]aigateway.PackageOrder, 0, limit)
	for rows.Next() {
		order, _, err := scanAIGatewayOrder(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *order)
	}
	return &aigateway.Page[aigateway.PackageOrder]{Items: items, Total: total, Page: page, Limit: limit}, rows.Err()
}

// PurchaseAIGatewayPackage 用余额买额度包：校验在售与限购、扣余额、建批次与订单，单事务。
// 同一用户同一 requestId 重放返回原订单；编号用在了别的包上时 ErrAIGatewayRequestIDReused。
func (r *Repository) PurchaseAIGatewayPackage(ctx context.Context, in aigateway.PurchaseInput) (*aigateway.PurchaseResult, error) {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	ledger, err := lockAIGatewayUserTx(ctx, tx, in.AppID, in.UserID)
	if err != nil {
		return nil, err
	}
	// 幂等：锁住用户行之后查，同一用户的购买彼此串行，查到即原单。
	if existing, _, err := getAIGatewayOrderQ(ctx, tx, `o.user_id = $1 AND o.request_id = $2`, in.UserID, in.RequestID); err == nil {
		if existing.PackageID != in.PackageID {
			return nil, ErrAIGatewayRequestIDReused
		}
		result, err := r.purchaseResultTx(ctx, tx, ledger, existing, now)
		if err != nil {
			return nil, err
		}
		result.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		tx = nil
		return result, nil
	} else if !errors.Is(err, ErrAIGatewayOrderNotFound) {
		return nil, err
	}

	pkg, err := scanAIGatewayPackage(tx.QueryRow(ctx, `SELECT `+aiGatewayPackageColumns+` FROM ai_gateway_packages p
WHERE p.appid = $1 AND p.id = $2 FOR UPDATE`, in.AppID, in.PackageID))
	if err != nil {
		return nil, err
	}
	profile, err := aiGatewayProfileQ(ctx, tx, in.AppID, in.UserID, isVipAt(ledger.vipExpire, ledger.lifetime, now))
	if err != nil {
		return nil, err
	}
	member := in.Settings.Member(profile)
	counts, err := aiGatewayPurchaseCountsQ(ctx, tx, in.AppID, in.UserID, pkg.ID, MonthStart(now, in.Settings.Location()))
	if err != nil {
		return nil, err
	}
	bought := counts[pkg.ID]
	if ok, reason := aigateway.PackageAvailability(*pkg, profile, in.Settings.Store.Enabled, bought[0], bought[1], now); !ok {
		return nil, &AIGatewayUnavailableError{Reason: reason}
	}
	quote := aigateway.QuotePackage(*pkg, member)
	if in.ExpectedPrice != nil && !in.ExpectedPrice.Equal(quote.FinalPrice) {
		return nil, ErrAIGatewayPriceChanged
	}
	if err := ledger.lockWallet(ctx); err != nil {
		return nil, err
	}
	cents := quote.FinalPrice.Shift(2).IntPart()
	if cents > ledger.walletCents {
		return nil, ErrAIGatewayWalletShort
	}

	orderNo := generateTransactionNo("AIP")
	walletTxnNo := ""
	if cents > 0 {
		change, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
			UserID: in.UserID, AppID: in.AppID, Amount: quote.FinalPrice.Neg(), Type: walletdomain.TxnTypeAIPackage,
			RelatedOrderNo: orderNo, IdempotencyKey: "aipkg:" + orderNo, Title: "购买 AI 额度包",
			Remark: pkg.Name, ClientIP: in.ClientIP,
			Metadata: map[string]any{"orderNo": orderNo, "packageId": pkg.ID, "points": quote.TotalPoints.String()},
		})
		if err != nil {
			if errors.Is(err, walletdomain.ErrInsufficientFunds) {
				return nil, ErrAIGatewayWalletShort
			}
			return nil, err
		}
		walletTxnNo = change.Transaction.TransactionNo
		ledger.walletCents -= cents
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_gateway_packages SET sold = sold + 1, updated_at = NOW() WHERE id = $1`, pkg.ID); err != nil {
		return nil, err
	}
	lot, err := grantAIGatewayLotTx(ctx, ledger, aigateway.LotGrantInput{
		AppID: in.AppID, UserID: in.UserID, Origin: aigateway.LotPurchase, Title: pkg.Name, Amount: quote.TotalPoints,
		ValidityDays: pkg.ValidityDays, Remark: "购买「" + pkg.Name + "」",
	}, orderNo, pkg.ID, aigateway.CreditPurchase, now)
	if err != nil {
		return nil, err
	}
	bonus := pkg.BonusPoints.Add(quote.MemberBonusPoints)
	if _, err := tx.Exec(ctx, `INSERT INTO ai_gateway_package_orders
(order_no, appid, user_id, package_id, package_name, list_price, price, discount_label, points, bonus_points, validity_days,
 expires_at, lot_id, wallet_txn_no, status, request_id, client_ip, created_at)
VALUES ($1,$2,$3,$4,$5,$6::numeric,$7::numeric,$8,$9,$10,$11,$12,$13,$14,'paid',$15,$16,$17)`,
		orderNo, in.AppID, in.UserID, pkg.ID, pkg.Name, pkg.Price.StringFixed(2), quote.FinalPrice.StringFixed(2),
		truncateColumn(quote.DiscountLabel, 64), aigateway.PointsToMicroFloor(pkg.Points), aigateway.PointsToMicroFloor(bonus),
		pkg.ValidityDays, lot.ExpiresAt, lot.ID, nullableString(walletTxnNo), in.RequestID,
		nullableString(truncateColumn(in.ClientIP, 64)), now); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrAIGatewayRequestIDReused
		}
		return nil, err
	}
	order, _, err := getAIGatewayOrderQ(ctx, tx, `o.order_no = $1`, orderNo)
	if err != nil {
		return nil, err
	}
	result, err := r.purchaseResultTx(ctx, tx, ledger, order, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *Repository) purchaseResultTx(ctx context.Context, tx pgx.Tx, ledger *aiGatewayLedger, order *aigateway.PackageOrder, now time.Time) (*aigateway.PurchaseResult, error) {
	if err := ledger.lockWallet(ctx); err != nil {
		return nil, err
	}
	result := &aigateway.PurchaseResult{Order: *order, Wallet: decimal.New(ledger.walletCents, -2)}
	if order.LotID > 0 {
		lot, err := getAIGatewayLotQ(ctx, tx, order.LotID, false)
		if err != nil {
			return nil, err
		}
		lot.UserID, lot.Account = 0, ""
		result.Lot = *lot
	}
	pack, err := aiGatewayPackTotalTx(ctx, tx, ledger.appID, ledger.userID, now)
	if err != nil {
		return nil, err
	}
	result.Balance = aigateway.MicroToPoints(saturatingAdd(pack, ledger.account.Credit))
	userFacingOrder(&result.Order)
	return result, nil
}

// GetAIGatewayOrder 一笔订单（管理端）。
func (r *Repository) GetAIGatewayOrder(ctx context.Context, appID int64, orderNo string) (*aigateway.PackageOrder, error) {
	order, _, err := getAIGatewayOrderQ(ctx, r.pool, `o.appid = $1 AND o.order_no = $2`, appID, orderNo)
	return order, err
}

// RefundAIGatewayOrder 管理员退额度包：按批次剩余比例退钱，批次作废，写钱包与额度流水。
// 批次正被调用暂扣时拒绝（等那次调用结算完再退，否则剩余比例会变）。
func (r *Repository) RefundAIGatewayOrder(ctx context.Context, in aigateway.OrderRefundInput) (*aigateway.PackageOrder, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	order, _, err := getAIGatewayOrderQ(ctx, tx, `o.appid = $1 AND o.order_no = $2`, in.AppID, in.OrderNo)
	if err != nil {
		return nil, err
	}
	ledger, err := lockAIGatewayUserTx(ctx, tx, in.AppID, order.UserID)
	if err != nil {
		return nil, err
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM ai_gateway_package_orders WHERE order_no = $1 FOR UPDATE`, in.OrderNo).Scan(&status); err != nil {
		return nil, err
	}
	if status != aigateway.OrderPaid {
		return nil, ErrAIGatewayOrderNotRefund
	}
	var remaining, amount int64
	if order.LotID > 0 {
		lot, err := getAIGatewayLotQ(ctx, tx, order.LotID, true)
		if err != nil {
			return nil, err
		}
		if lot.FrozenMicro > 0 {
			return nil, ErrAIGatewayOrderLotBusy
		}
		amount = lot.AmountMicro
		if lot.Status == aigateway.LotActive || lot.Status == aigateway.LotExhausted {
			if lot.ExpiresAt == nil || time.Now().Before(*lot.ExpiresAt) {
				remaining = lot.RemainingMicro
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_gateway_credit_lots SET status = 'revoked', remaining = 0, updated_at = NOW() WHERE id = $1`,
			lot.ID); err != nil {
			return nil, err
		}
		if lot.RemainingMicro > 0 {
			if err := ledger.insertLedger(ctx, aigateway.CreditOrderRefund, aigateway.BucketPack, -lot.RemainingMicro, lot.ID,
				"package_order", order.OrderNo, "额度包退款："+order.PackageName, in.Operator, ""); err != nil {
				return nil, err
			}
		}
	}
	refund := aigateway.RefundByRemaining(order.Price, remaining, amount)
	if refund.Sign() > 0 {
		reason := "额度包退款：" + order.PackageName
		if in.Reason != "" {
			reason += "（" + in.Reason + "）"
		}
		if _, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
			UserID: order.UserID, AppID: in.AppID, Amount: refund, Type: walletdomain.TxnTypeAIRefund,
			RelatedOrderNo: order.OrderNo, IdempotencyKey: "aipkg-refund:" + order.OrderNo, Title: "AI 额度包退款",
			Remark: reason, Operator: in.Operator,
			Restores: &walletdomain.RestoreRef{IdempotencyKey: "aipkg:" + order.OrderNo},
			Metadata: map[string]any{"orderNo": order.OrderNo},
		}); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_gateway_package_orders SET status = 'refunded', refund_amount = $2::numeric,
refund_points = $3, refund_reason = $4, refunded_by = $5, refunded_at = NOW() WHERE order_no = $1`,
		order.OrderNo, refund.StringFixed(2), remaining, nullableString(truncateColumn(in.Reason, 200)),
		nullableString(in.Operator)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_gateway_packages SET sold = GREATEST(sold - 1, 0), updated_at = NOW() WHERE id = $1`,
		order.PackageID); err != nil {
		return nil, err
	}
	refunded, _, err := getAIGatewayOrderQ(ctx, tx, `o.order_no = $1`, order.OrderNo)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return refunded, nil
}

// ── 批次 ──

// ListAIGatewayLots 批次列表。
func (r *Repository) ListAIGatewayLots(ctx context.Context, query aigateway.LotQuery) (*aigateway.Page[aigateway.CreditLot], error) {
	page, limit := aiGatewayPageLimit(query.Page, query.Limit, 100)
	where := []string{"l.appid = $1"}
	args := []any{query.AppID}
	add := func(cond string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(cond, "$?", fmt.Sprintf("$%d", len(args))))
	}
	if query.UserID > 0 {
		add("l.user_id = $?", query.UserID)
	}
	if query.Origin != "" {
		add("l.origin = $?", query.Origin)
	}
	switch {
	case query.Scope == "active":
		where = append(where, "l.status = 'active' AND (l.remaining > 0 OR l.frozen > 0) AND (l.expires_at IS NULL OR l.expires_at > NOW())")
	case query.Status == aigateway.LotExhausted:
		where = append(where, "l.status = 'active' AND l.remaining = 0 AND l.frozen = 0")
	case query.Status == aigateway.LotActive:
		where = append(where, "l.status = 'active' AND (l.remaining > 0 OR l.frozen > 0)")
	case query.Status != "":
		add("l.status = $?", query.Status)
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_credit_lots l WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	order := ` ORDER BY l.id DESC`
	if query.Scope == "active" {
		order = ` ORDER BY l.expires_at ASC NULLS LAST, l.id ASC`
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+aiGatewayLotColumns+` FROM ai_gateway_credit_lots l LEFT JOIN users u ON u.id = l.user_id
WHERE `+clause+order+fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]aigateway.CreditLot, 0, limit)
	for rows.Next() {
		lot, err := scanAIGatewayLot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *lot)
	}
	return &aigateway.Page[aigateway.CreditLot]{Items: items, Total: total, Page: page, Limit: limit}, rows.Err()
}

// GrantAIGatewayLot 管理员发放限期额度。RequestID 相同只生效一次（同人同量重放返回原批次）。
func (r *Repository) GrantAIGatewayLot(ctx context.Context, in aigateway.LotGrantInput) (*aigateway.CreditLot, error) {
	now := time.Now()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	ledger, err := lockAIGatewayUserTx(ctx, tx, in.AppID, in.UserID)
	if err != nil {
		return nil, err
	}
	if in.RequestID != "" {
		var id, userID, amount int64
		err := tx.QueryRow(ctx, `SELECT id, user_id, amount FROM ai_gateway_credit_lots WHERE appid = $1 AND request_id = $2`,
			in.AppID, in.RequestID).Scan(&id, &userID, &amount)
		if err == nil {
			if userID != in.UserID || amount != aigateway.PointsToMicroFloor(in.Amount) {
				return nil, ErrAIGatewayRequestIDReused
			}
			lot, err := getAIGatewayLotQ(ctx, tx, id, false)
			if err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			tx = nil
			return lot, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	kind := aigateway.CreditGrant
	if in.Origin == aigateway.LotAdmin {
		kind = aigateway.CreditAdjust
	}
	lot, err := grantAIGatewayLotTx(ctx, ledger, in, "", 0, kind, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return lot, nil
}

// RevokeAIGatewayLot 作废一个批次的剩余额度（暂扣中的部分照常结算，结算后放回的由到期清理清零）。
func (r *Repository) RevokeAIGatewayLot(ctx context.Context, appID, lotID int64, reason, operator string) (*aigateway.CreditLot, error) {
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
	if err := tx.QueryRow(ctx, `SELECT user_id FROM ai_gateway_credit_lots WHERE id = $1 AND appid = $2`, lotID, appID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAIGatewayLotNotFound
		}
		return nil, err
	}
	ledger, err := lockAIGatewayUserTx(ctx, tx, appID, userID)
	if err != nil {
		return nil, err
	}
	lot, err := getAIGatewayLotQ(ctx, tx, lotID, true)
	if err != nil {
		return nil, err
	}
	if lot.Status != aigateway.LotActive && lot.Status != aigateway.LotExhausted {
		return nil, ErrAIGatewayLotUnavailable
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_gateway_credit_lots SET status = 'revoked', remaining = 0, updated_at = NOW() WHERE id = $1`, lotID); err != nil {
		return nil, err
	}
	if lot.RemainingMicro > 0 {
		remark := "作废额度：" + lot.Title
		if reason != "" {
			remark += "（" + reason + "）"
		}
		if err := ledger.insertLedger(ctx, aigateway.CreditRevoke, aigateway.BucketPack, -lot.RemainingMicro, lotID, "lot",
			fmt.Sprint(lotID), remark, operator, ""); err != nil {
			return nil, err
		}
	}
	revoked, err := getAIGatewayLotQ(ctx, tx, lotID, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return revoked, nil
}

// ClaimAIGatewayWelcome 领新人礼：每人一次。
func (r *Repository) ClaimAIGatewayWelcome(ctx context.Context, appID, userID int64, gift aigateway.WelcomeGift) (*aigateway.WelcomeResult, error) {
	now := time.Now()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	ledger, err := lockAIGatewayUserTx(ctx, tx, appID, userID)
	if err != nil {
		return nil, err
	}
	var claimed *time.Time
	if err := tx.QueryRow(ctx, `SELECT welcome_claimed_at FROM ai_gateway_accounts WHERE user_id = $1`, userID).Scan(&claimed); err != nil {
		return nil, err
	}
	if claimed != nil || !gift.Enabled || gift.Amount.Sign() <= 0 {
		return nil, ErrAIGatewayWelcomeClaimed
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_gateway_accounts SET welcome_claimed_at = $2, updated_at = NOW() WHERE user_id = $1`, userID, now); err != nil {
		return nil, err
	}
	lot, err := grantAIGatewayLotTx(ctx, ledger, aigateway.LotGrantInput{
		AppID: appID, UserID: userID, Origin: aigateway.LotWelcome, Title: "新人礼", Amount: gift.Amount,
		ValidityDays: gift.ValidityDays, Remark: "领取新人礼",
	}, "", 0, aigateway.CreditWelcome, now)
	if err != nil {
		return nil, err
	}
	pack, err := aiGatewayPackTotalTx(ctx, tx, appID, userID, now)
	if err != nil {
		return nil, err
	}
	result := &aigateway.WelcomeResult{Lot: *lot, Balance: aigateway.MicroToPoints(saturatingAdd(pack, ledger.account.Credit))}
	result.Lot.UserID, result.Lot.Account = 0, ""
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

// ── 到期清理 ──

// SweepAIGatewayLots 把过期的批次清零、作废 / 过期后又被结算放回的余量清零，各写一条流水。返回处理的批次数。
// 多实例同时跑也安全：逐个批次在锁住用户行之后重查。
func (r *Repository) SweepAIGatewayLots(ctx context.Context, now time.Time, limit int) (int, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, appid, user_id FROM ai_gateway_credit_lots
WHERE (status = 'active' AND expires_at IS NOT NULL AND expires_at <= $1) OR (status <> 'active' AND remaining > 0)
ORDER BY id LIMIT $2`, now, limit)
	if err != nil {
		return 0, err
	}
	type target struct{ id, appID, userID int64 }
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.appID, &t.userID); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	done := 0
	for _, t := range targets {
		ok, err := r.sweepAIGatewayLot(ctx, t.appID, t.userID, t.id, now)
		if err != nil {
			return done, err
		}
		if ok {
			done++
		}
	}
	return done, nil
}

func (r *Repository) sweepAIGatewayLot(ctx context.Context, appID, userID, lotID int64, now time.Time) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ledger, err := lockAIGatewayUserTx(ctx, tx, appID, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return false, nil
		}
		return false, err
	}
	var remaining int64
	var status, title string
	var expiresAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT remaining, status, title, expires_at FROM ai_gateway_credit_lots WHERE id = $1 FOR UPDATE`, lotID).
		Scan(&remaining, &status, &title, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	expired := status == aigateway.LotActive && expiresAt != nil && !now.Before(*expiresAt)
	if !expired && (status == aigateway.LotActive || remaining == 0) {
		return false, nil
	}
	next := status
	if expired {
		next = aigateway.LotExpired
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_gateway_credit_lots SET status = $2, remaining = 0, updated_at = NOW() WHERE id = $1`,
		lotID, next); err != nil {
		return false, err
	}
	if remaining > 0 {
		kind, remark := aigateway.CreditExpire, "额度到期："+title
		if next == aigateway.LotRevoked {
			kind, remark = aigateway.CreditRevoke, "额度已作废："+title
		}
		if err := ledger.insertLedger(ctx, kind, aigateway.BucketPack, -remaining, lotID, "lot", fmt.Sprint(lotID), remark, "", ""); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

// ── 统计 ──

// AIGatewayCommerceStats 额度生意统计：最近 days 天（按 timezone 分日）。
func (r *Repository) AIGatewayCommerceStats(ctx context.Context, appID int64, days int, timezone string, now time.Time) (*aigateway.CommerceStats, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	out := &aigateway.CommerceStats{Days: days, ByPackage: []aigateway.PackageTotal{}, Daily: []aigateway.CommerceDaily{},
		Expiry: []aigateway.ExpiryBucket{}, Windows: []aigateway.WindowUsage{}}

	var revenue, refunded string
	var pointsSold int64
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(price), 0)::text, COALESCE(SUM(refund_amount), 0)::text,
COUNT(*), COUNT(DISTINCT user_id), COALESCE(SUM(points + bonus_points), 0)::bigint
FROM ai_gateway_package_orders WHERE appid = $1 AND created_at >= $2`, appID, start).
		Scan(&revenue, &refunded, &out.Orders, &out.Buyers, &pointsSold); err != nil {
		return nil, err
	}
	out.Revenue = decimal.RequireFromString(revenue)
	out.Refunded = decimal.RequireFromString(refunded)
	out.PointsSold = aigateway.MicroToPoints(pointsSold)

	var outstanding, expiring, permanent int64
	if err := r.pool.QueryRow(ctx, `SELECT
  COALESCE(SUM(remaining + frozen) FILTER (WHERE status = 'active' AND (expires_at IS NULL OR expires_at > $2)), 0)::bigint,
  COALESCE(SUM(remaining + frozen) FILTER (WHERE status = 'active' AND expires_at > $2 AND expires_at <= $2::timestamptz + INTERVAL '7 days'), 0)::bigint
FROM ai_gateway_credit_lots WHERE appid = $1`, appID, now).Scan(&outstanding, &expiring); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(credit + credit_frozen), 0)::bigint FROM ai_gateway_accounts WHERE appid = $1`,
		appID).Scan(&permanent); err != nil {
		return nil, err
	}
	out.Outstanding = aigateway.MicroToPoints(outstanding)
	out.ExpiringSoon = aigateway.MicroToPoints(expiring)
	out.OutstandingPermanent = aigateway.MicroToPoints(permanent)

	rows, err := r.pool.Query(ctx, `SELECT o.package_id, MAX(o.package_name), COUNT(*), COALESCE(SUM(o.price - o.refund_amount), 0)::text,
COALESCE(SUM(o.points + o.bonus_points), 0)::bigint
FROM ai_gateway_package_orders o WHERE o.appid = $1 AND o.created_at >= $2 GROUP BY o.package_id ORDER BY 4 DESC`, appID, start)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item aigateway.PackageTotal
		var amount string
		var points int64
		if err := rows.Scan(&item.PackageID, &item.Name, &item.Orders, &amount, &points); err != nil {
			rows.Close()
			return nil, err
		}
		item.Revenue = decimal.RequireFromString(amount)
		item.Points = aigateway.MicroToPoints(points)
		out.ByPackage = append(out.ByPackage, item)
	}
	rows.Close()

	daily := map[string]*aigateway.CommerceDaily{}
	for i := 0; i < days; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		out.Daily = append(out.Daily, aigateway.CommerceDaily{Date: date, Revenue: decimal.Zero, Refunded: decimal.Zero})
	}
	for i := range out.Daily {
		daily[out.Daily[i].Date] = &out.Daily[i]
	}
	rows, err = r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD'), COUNT(*), COALESCE(SUM(price), 0)::text,
COALESCE(SUM(refund_amount), 0)::text FROM ai_gateway_package_orders WHERE appid = $1 AND created_at >= $2 GROUP BY 1`,
		appID, start, loc.String())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var date, amount, refund string
		var orders int64
		if err := rows.Scan(&date, &orders, &amount, &refund); err != nil {
			rows.Close()
			return nil, err
		}
		if item, ok := daily[date]; ok {
			item.Orders = orders
			item.Revenue = decimal.RequireFromString(amount)
			item.Refunded = decimal.RequireFromString(refund)
		}
	}
	rows.Close()

	var d7, d30, d90, later, never int64
	if err := r.pool.QueryRow(ctx, `SELECT
  COALESCE(SUM(remaining + frozen) FILTER (WHERE expires_at > $2 AND expires_at <= $2::timestamptz + INTERVAL '7 days'), 0)::bigint,
  COALESCE(SUM(remaining + frozen) FILTER (WHERE expires_at > $2::timestamptz + INTERVAL '7 days' AND expires_at <= $2::timestamptz + INTERVAL '30 days'), 0)::bigint,
  COALESCE(SUM(remaining + frozen) FILTER (WHERE expires_at > $2::timestamptz + INTERVAL '30 days' AND expires_at <= $2::timestamptz + INTERVAL '90 days'), 0)::bigint,
  COALESCE(SUM(remaining + frozen) FILTER (WHERE expires_at > $2::timestamptz + INTERVAL '90 days'), 0)::bigint,
  COALESCE(SUM(remaining + frozen) FILTER (WHERE expires_at IS NULL), 0)::bigint
FROM ai_gateway_credit_lots WHERE appid = $1 AND status = 'active'`, appID, now).Scan(&d7, &d30, &d90, &later, &never); err != nil {
		return nil, err
	}
	for _, item := range []struct {
		bucket string
		micro  int64
	}{{"7d", d7}, {"30d", d30}, {"90d", d90}, {"later", later}, {"never", never}} {
		out.Expiry = append(out.Expiry, aigateway.ExpiryBucket{Bucket: item.bucket, Amount: aigateway.MicroToPoints(item.micro)})
	}

	// 按时段：调用暂扣时快照的 billing.pricing.window；让利 = charged / time − charged。
	rows, err = r.pool.Query(ctx, `SELECT billing #>> '{pricing,window,id}', MAX(billing #>> '{pricing,window,name}'), COUNT(*),
COALESCE(SUM(charged), 0)::bigint,
COALESCE(SUM(charged / NULLIF((billing #>> '{pricing,time}')::numeric, 0) - charged), 0)::bigint
FROM ai_gateway_requests
WHERE appid = $1 AND created_at >= $2 AND status IN ('succeeded', 'aborted') AND billing #>> '{pricing,window,id}' IS NOT NULL
GROUP BY 1 ORDER BY 3 DESC`, appID, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item aigateway.WindowUsage
		var charged, saved int64
		if err := rows.Scan(&item.WindowID, &item.Name, &item.Requests, &charged, &saved); err != nil {
			return nil, err
		}
		item.Charged = aigateway.MicroToPoints(charged)
		item.Saved = aigateway.MicroToPoints(saved)
		out.Windows = append(out.Windows, item)
	}
	return out, rows.Err()
}
