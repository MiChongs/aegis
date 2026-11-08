package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
)

// 积分商城：商品、兑换（单事务：锁用户 → 判定 → 扣库存 → 扣积分 → 发放 → 记账）、人工发放的处理。
// 规则见 docs/points-store.md。

var (
	ErrPointsStoreItemNotFound       = errors.New("points store item not found")
	ErrPointsStoreRedemptionNotFound = errors.New("points store redemption not found")
	// ErrPointsStoreRedemptionState 只有待发放的兑换能被标记发放或取消
	ErrPointsStoreRedemptionState = errors.New("points store redemption is not pending")
)

// PointsStoreUnavailableError 这件商品此刻不能被这个人兑换（判定结论随错误带出）。
type PointsStoreUnavailableError struct {
	Availability pointstore.Availability
}

func (e *PointsStoreUnavailableError) Error() string {
	return "points store item unavailable: " + e.Availability.Reason
}

func pointsStoreUnavailable(reason string) error {
	return &PointsStoreUnavailableError{Availability: pointstore.Availability{
		Reason: reason, Message: pointstore.ReasonMessage(reason),
	}}
}

// ── 开关 ──

// GetPointsStoreSettings 读开关；没保存过时返回关闭、configured=false。
func (r *Repository) GetPointsStoreSettings(ctx context.Context, appID int64) (*pointstore.Settings, error) {
	settings := pointstore.Settings{AppID: appID}
	err := r.pool.QueryRow(ctx, `SELECT enabled, COALESCE(updated_by, ''), updated_at
FROM points_store_settings WHERE appid = $1`, appID).Scan(&settings.Enabled, &settings.UpdatedBy, &settings.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &settings, nil
	}
	if err != nil {
		return nil, err
	}
	settings.Configured = true
	return &settings, nil
}

// SavePointsStoreSettings 保存开关。
func (r *Repository) SavePointsStoreSettings(ctx context.Context, appID int64, enabled bool, operator string) (*pointstore.Settings, error) {
	settings := pointstore.Settings{AppID: appID, Configured: true}
	err := r.pool.QueryRow(ctx, `INSERT INTO points_store_settings (appid, enabled, updated_by)
VALUES ($1, $2, $3)
ON CONFLICT (appid) DO UPDATE SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = NOW()
RETURNING enabled, COALESCE(updated_by, ''), updated_at`, appID, enabled, nullableString(operator)).
		Scan(&settings.Enabled, &settings.UpdatedBy, &settings.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

// ── 商品 ──

const pointsStoreItemColumns = `i.id, i.appid, i.title, i.subtitle, i.description, i.category, i.badge, i.cost, i.original_cost,
i.rewards, i.stock, i.sold_count, i.limit_period, i.limit_count, i.audience, i.min_level, i.starts_at, i.ends_at,
i.status, i.sort_order, COALESCE(i.created_by, ''), i.created_at, i.updated_at,
i.restock_period, i.restock_quantity, i.stock_window_start, i.global_limit_period, i.global_limit_count,
i.user_total_limit, i.daily_open_minute, i.daily_close_minute`

func scanPointsStoreItem(row interface{ Scan(dest ...any) error }, extra ...any) (*pointstore.Item, error) {
	var item pointstore.Item
	var rewards []byte
	dest := []any{&item.ID, &item.AppID, &item.Title, &item.Subtitle, &item.Description, &item.Category, &item.Badge,
		&item.Cost, &item.OriginalCost, &rewards, &item.Stock, &item.SoldCount, &item.LimitPeriod, &item.LimitCount,
		&item.Audience, &item.MinLevel, &item.StartsAt, &item.EndsAt, &item.Status, &item.SortOrder,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
		&item.RestockPeriod, &item.RestockQuantity, &item.StockWindowStart, &item.GlobalLimitPeriod, &item.GlobalLimitCount,
		&item.UserTotalLimit, &item.DailyOpenMinute, &item.DailyCloseMinute}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPointsStoreItemNotFound
		}
		return nil, err
	}
	item.Rewards = []pointstore.Reward{}
	if len(rewards) > 0 {
		if err := json.Unmarshal(rewards, &item.Rewards); err != nil {
			return nil, fmt.Errorf("decode points store rewards: %w", err)
		}
	}
	return &item, nil
}

// ListPointsStoreItems 应用的商品，按排序值、再按创建先后。activeOnly 只列上架的。
func (r *Repository) ListPointsStoreItems(ctx context.Context, appID int64, activeOnly bool) ([]pointstore.Item, error) {
	query := `SELECT ` + pointsStoreItemColumns + ` FROM points_store_items i WHERE i.appid = $1`
	if activeOnly {
		query += ` AND i.status = 'active'`
	}
	rows, err := r.pool.Query(ctx, query+` ORDER BY i.sort_order ASC, i.id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]pointstore.Item, 0, 16)
	for rows.Next() {
		item, err := scanPointsStoreItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ListPointsStoreAdminItems 管理端商品列表：每件商品带兑换笔数、人数、消耗积分与待发放笔数。
func (r *Repository) ListPointsStoreAdminItems(ctx context.Context, appID int64) ([]pointstore.AdminItem, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+pointsStoreItemColumns+`,
       COALESCE(s.redemptions, 0), COALESCE(s.redeemers, 0), COALESCE(s.points, 0), COALESCE(s.pending, 0)
FROM points_store_items i
LEFT JOIN (
    SELECT item_id,
           COUNT(*) FILTER (WHERE status <> 'cancelled') AS redemptions,
           COUNT(DISTINCT user_id) FILTER (WHERE status <> 'cancelled') AS redeemers,
           COALESCE(SUM(cost) FILTER (WHERE status <> 'cancelled'), 0) AS points,
           COUNT(*) FILTER (WHERE status = 'pending') AS pending
    FROM points_store_redemptions
    WHERE appid = $1 AND item_id IS NOT NULL
    GROUP BY item_id
) s ON s.item_id = i.id
WHERE i.appid = $1
ORDER BY i.sort_order ASC, i.id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]pointstore.AdminItem, 0, 16)
	for rows.Next() {
		var row pointstore.AdminItem
		item, err := scanPointsStoreItem(rows, &row.Redemptions, &row.Redeemers, &row.PointsSpent, &row.Pending)
		if err != nil {
			return nil, err
		}
		row.Item = *item
		items = append(items, row)
	}
	return items, rows.Err()
}

// GetPointsStoreItem 取一件商品。
func (r *Repository) GetPointsStoreItem(ctx context.Context, appID, itemID int64) (*pointstore.Item, error) {
	return scanPointsStoreItem(r.pool.QueryRow(ctx,
		`SELECT `+pointsStoreItemColumns+` FROM points_store_items i WHERE i.id = $1 AND i.appid = $2`, itemID, appID))
}

// CreatePointsStoreItem 新建商品（输入已由服务层清洗与校验）。
func (r *Repository) CreatePointsStoreItem(ctx context.Context, input pointstore.ItemInput) (*pointstore.Item, error) {
	rewards, _ := json.Marshal(input.Rewards)
	return scanPointsStoreItem(r.pool.QueryRow(ctx, `INSERT INTO points_store_items AS i
(appid, title, subtitle, description, category, badge, cost, original_cost, rewards, stock,
 limit_period, limit_count, audience, min_level, starts_at, ends_at, status, sort_order, created_by,
 restock_period, restock_quantity, stock_window_start, global_limit_period, global_limit_count,
 user_total_limit, daily_open_minute, daily_close_minute)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
RETURNING `+pointsStoreItemColumns,
		input.AppID, input.Title, input.Subtitle, input.Description, input.Category, input.Badge,
		input.Cost, input.OriginalCost, rewards, input.Stock, input.LimitPeriod, input.LimitCount,
		input.Audience, input.MinLevel, input.StartsAt, input.EndsAt, input.Status, input.SortOrder,
		nullableString(input.Operator),
		input.RestockPeriod, input.RestockQuantity, input.StockWindowStart, input.GlobalLimitPeriod, input.GlobalLimitCount,
		input.UserTotalLimit, input.DailyOpenMinute, input.DailyCloseMinute))
}

// UpdatePointsStoreItem 整体更新商品。已兑出的记录存的是快照，不受影响。
func (r *Repository) UpdatePointsStoreItem(ctx context.Context, itemID int64, input pointstore.ItemInput) (*pointstore.Item, error) {
	rewards, _ := json.Marshal(input.Rewards)
	return scanPointsStoreItem(r.pool.QueryRow(ctx, `UPDATE points_store_items AS i SET
title = $3, subtitle = $4, description = $5, category = $6, badge = $7, cost = $8, original_cost = $9,
rewards = $10, stock = $11, limit_period = $12, limit_count = $13, audience = $14, min_level = $15,
starts_at = $16, ends_at = $17, status = $18, sort_order = $19,
restock_period = $20, restock_quantity = $21, stock_window_start = $22, global_limit_period = $23,
global_limit_count = $24, user_total_limit = $25, daily_open_minute = $26, daily_close_minute = $27, updated_at = NOW()
WHERE i.id = $1 AND i.appid = $2
RETURNING `+pointsStoreItemColumns,
		itemID, input.AppID, input.Title, input.Subtitle, input.Description, input.Category, input.Badge,
		input.Cost, input.OriginalCost, rewards, input.Stock, input.LimitPeriod, input.LimitCount,
		input.Audience, input.MinLevel, input.StartsAt, input.EndsAt, input.Status, input.SortOrder,
		input.RestockPeriod, input.RestockQuantity, input.StockWindowStart, input.GlobalLimitPeriod, input.GlobalLimitCount,
		input.UserTotalLimit, input.DailyOpenMinute, input.DailyCloseMinute))
}

// SetPointsStoreItemStatus 上架 / 下架。
func (r *Repository) SetPointsStoreItemStatus(ctx context.Context, appID, itemID int64, status string) (*pointstore.Item, error) {
	return scanPointsStoreItem(r.pool.QueryRow(ctx, `UPDATE points_store_items AS i SET status = $3, updated_at = NOW()
WHERE i.id = $1 AND i.appid = $2 RETURNING `+pointsStoreItemColumns, itemID, appID, status))
}

// ReorderPointsStoreItems 按给定顺序重写排序值（10、20、30……），只动排序与更新时间，
// 不碰库存等字段，免得和并发中的兑换互相覆盖。不属于该应用的编号被忽略。
func (r *Repository) ReorderPointsStoreItems(ctx context.Context, appID int64, itemIDs []int64) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE points_store_items AS i SET sort_order = o.ord * 10, updated_at = NOW()
FROM unnest($2::bigint[]) WITH ORDINALITY AS o(id, ord)
WHERE i.id = o.id AND i.appid = $1 AND i.sort_order IS DISTINCT FROM o.ord * 10`, appID, itemIDs)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeletePointsStoreItem 删除商品。兑换记录保留（item_id 置空，标题与权益看快照），
// 待发放的兑换照常处理。
func (r *Repository) DeletePointsStoreItem(ctx context.Context, appID, itemID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM points_store_items WHERE id = $1 AND appid = $2`, itemID, appID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPointsStoreItemNotFound
	}
	return nil
}

// PointsStoreCategories 应用里已经用过的分类（控制台的分类输入据此补全）。
func (r *Repository) PointsStoreCategories(ctx context.Context, appID int64) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT category FROM points_store_items
WHERE appid = $1 AND category <> '' GROUP BY category ORDER BY MIN(sort_order), MIN(id)`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 4)
	for rows.Next() {
		var category string
		if err := rows.Scan(&category); err != nil {
			return nil, err
		}
		out = append(out, category)
	}
	return out, rows.Err()
}

// ── 判定事实 ──

// pointsStoreUsageSQL 某人对商品的已兑次数（不含已取消），各周期分别计数。
const pointsStoreUsageSQL = `SELECT item_id, COUNT(*),
       COUNT(*) FILTER (WHERE created_at >= $3),
       COUNT(*) FILTER (WHERE created_at >= $4),
       COUNT(*) FILTER (WHERE created_at >= $5)
FROM points_store_redemptions
WHERE appid = $1 AND user_id = $2 AND status <> 'cancelled' AND item_id IS NOT NULL`

// PointsStoreUsage 某人对每件商品的已兑次数。
func (r *Repository) PointsStoreUsage(ctx context.Context, appID, userID int64, windows pointstore.Windows) (map[int64]pointstore.Usage, error) {
	rows, err := r.pool.Query(ctx, pointsStoreUsageSQL+` GROUP BY item_id`,
		appID, userID, windows.Day, windows.Week, windows.Month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]pointstore.Usage{}
	for rows.Next() {
		var itemID int64
		var usage pointstore.Usage
		if err := rows.Scan(&itemID, &usage.Total, &usage.Day, &usage.Week, &usage.Month); err != nil {
			return nil, err
		}
		out[itemID] = usage
	}
	return out, rows.Err()
}

// pointsStoreGlobalUsageSQL 所有人对商品的已兑次数（不含已取消），各周期分别计数：判全站名额。
const pointsStoreGlobalUsageSQL = `SELECT item_id, COUNT(*),
       COUNT(*) FILTER (WHERE created_at >= $2),
       COUNT(*) FILTER (WHERE created_at >= $3),
       COUNT(*) FILTER (WHERE created_at >= $4)
FROM points_store_redemptions
WHERE appid = $1 AND status <> 'cancelled' AND item_id IS NOT NULL`

// PointsStoreGlobalUsage 每件设了全站名额的商品被所有人兑了几次。只数设了名额的商品，
// 没设名额的商品不需要这个数，免得每次打开商城都把全表扫一遍。
func (r *Repository) PointsStoreGlobalUsage(ctx context.Context, appID int64, windows pointstore.Windows) (map[int64]pointstore.Counts, error) {
	rows, err := r.pool.Query(ctx, pointsStoreGlobalUsageSQL+`
  AND item_id IN (SELECT id FROM points_store_items WHERE appid = $1 AND global_limit_count > 0)
GROUP BY item_id`, appID, windows.Day, windows.Week, windows.Month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]pointstore.Counts{}
	for rows.Next() {
		var itemID int64
		var counts pointstore.Counts
		if err := rows.Scan(&itemID, &counts.Total, &counts.Day, &counts.Week, &counts.Month); err != nil {
			return nil, err
		}
		out[itemID] = counts
	}
	return out, rows.Err()
}

func pointsStoreItemGlobalUsageTx(ctx context.Context, tx pgx.Tx, appID, itemID int64, windows pointstore.Windows) (pointstore.Counts, error) {
	var counts pointstore.Counts
	var ignored int64
	err := tx.QueryRow(ctx, pointsStoreGlobalUsageSQL+` AND item_id = $5 GROUP BY item_id`,
		appID, windows.Day, windows.Week, windows.Month, itemID).
		Scan(&ignored, &counts.Total, &counts.Day, &counts.Week, &counts.Month)
	if errors.Is(err, pgx.ErrNoRows) {
		return pointstore.Counts{}, nil
	}
	return counts, err
}

func pointsStoreItemUsageTx(ctx context.Context, tx pgx.Tx, appID, userID, itemID int64, windows pointstore.Windows) (pointstore.Usage, error) {
	var usage pointstore.Usage
	var ignored int64
	err := tx.QueryRow(ctx, pointsStoreUsageSQL+` AND item_id = $6 GROUP BY item_id`,
		appID, userID, windows.Day, windows.Week, windows.Month, itemID).
		Scan(&ignored, &usage.Total, &usage.Day, &usage.Week, &usage.Month)
	if errors.Is(err, pgx.ErrNoRows) {
		return pointstore.Usage{}, nil
	}
	return usage, err
}

// PointsStoreCatalogRefs 应用的会员套餐与功能目录（含停用的），判定与展示共用。
func (r *Repository) PointsStoreCatalogRefs(ctx context.Context, appID int64) (map[int64]pointstore.PlanRef, map[string]pointstore.FeatureRef, error) {
	return pointsStoreCatalogRefs(ctx, r.pool, appID)
}

// pointsStoreCatalogPlansSQL / pointsStoreCatalogFeaturesSQL 应用的会员套餐与功能目录（$1 appid）。
const (
	pointsStoreCatalogPlansSQL    = `SELECT ` + vipPlanColumns + ` FROM vip_plans WHERE appid = $1`
	pointsStoreCatalogFeaturesSQL = `SELECT tag, name, is_active FROM vip_features WHERE appid = $1`
)

func pointsStoreCatalogRefs(ctx context.Context, q queryExecutor, appID int64) (map[int64]pointstore.PlanRef, map[string]pointstore.FeatureRef, error) {
	plans, features := map[int64]pointstore.PlanRef{}, map[string]pointstore.FeatureRef{}
	rows, err := q.Query(ctx, pointsStoreCatalogPlansSQL, appID)
	if err != nil {
		return nil, nil, err
	}
	if err := scanCatalogPlans(rows, plans); err != nil {
		return nil, nil, err
	}
	featureRows, err := q.Query(ctx, pointsStoreCatalogFeaturesSQL, appID)
	if err != nil {
		return nil, nil, err
	}
	if err := scanCatalogFeatures(featureRows, features); err != nil {
		return nil, nil, err
	}
	return plans, features, nil
}

// scanCatalogPlans 解析 pointsStoreCatalogPlansSQL（读完并关闭 rows；单发与批量管道共用）。
func scanCatalogPlans(rows pgx.Rows, plans map[int64]pointstore.PlanRef) error {
	defer rows.Close()
	for rows.Next() {
		plan, err := scanVipPlan(rows)
		if err != nil {
			return err
		}
		if plan == nil {
			continue
		}
		plans[plan.ID] = pointstore.PlanRef{
			ID: plan.ID, Name: plan.Name, DurationDays: plan.DurationDays, Lifetime: plan.Lifetime,
			Trial: plan.IsTrial(), Features: vipdomain.NormalizeFeatureTags(plan.Features),
		}
	}
	return rows.Err()
}

// scanCatalogFeatures 解析 pointsStoreCatalogFeaturesSQL（读完并关闭 rows）。
func scanCatalogFeatures(rows pgx.Rows, features map[string]pointstore.FeatureRef) error {
	defer rows.Close()
	for rows.Next() {
		var ref pointstore.FeatureRef
		if err := rows.Scan(&ref.Tag, &ref.Name, &ref.IsActive); err != nil {
			return err
		}
		features[ref.Tag] = ref
	}
	return rows.Err()
}

// PointsStoreUserFacts 一个人兑换判定所需的事实（列表页用；兑换时在事务里另取一份）。
func (r *Repository) PointsStoreUserFacts(ctx context.Context, appID, userID int64) (*pointstore.UserFacts, error) {
	var integral, experience int64
	if err := r.pool.QueryRow(ctx, `SELECT integral, experience FROM users WHERE id = $1 AND appid = $2`,
		userID, appID).Scan(&integral, &experience); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	entitlement, err := r.GetVipEntitlementFacts(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	return r.assemblePointsStoreFacts(ctx, r.pool, appID, userID, integral, experience, entitlement)
}

func (r *Repository) assemblePointsStoreFacts(ctx context.Context, q queryExecutor, appID, userID int64,
	integral, experience int64, entitlementFacts *vipdomain.EvalInput) (*pointstore.UserFacts, error) {
	now := time.Now()
	entitlement := vipdomain.Evaluate(*entitlementFacts, now)
	level, _, err := r.userLevelQ(ctx, q, appID, userID, experience)
	if err != nil {
		return nil, err
	}
	plans, features, err := pointsStoreCatalogRefs(ctx, q, appID)
	if err != nil {
		return nil, err
	}
	return &pointstore.UserFacts{
		Integral:   integral,
		Level:      level,
		IsVip:      entitlement.IsVIP,
		IsLifetime: entitlement.IsLifetime,
		Holdings:   vipdomain.HoldingsOf(*entitlementFacts, now),
		Plans:      plans,
		Features:   features,
	}, nil
}

// ── 兑换 ──

const pointsStoreRedemptionColumns = `r.id, r.redemption_no, r.appid, r.user_id, r.item_id, r.item_title, r.cost,
r.rewards, r.results, r.status, COALESCE(r.integral_transaction_no, ''), COALESCE(r.refund_transaction_no, ''),
COALESCE(r.request_id, ''), COALESCE(r.note, ''), COALESCE(r.handled_by, ''), r.handled_at,
COALESCE(r.device_id, ''), COALESCE(r.client_ip, ''), r.created_at, r.updated_at`

func scanPointsStoreRedemption(row interface{ Scan(dest ...any) error }, extra ...any) (*pointstore.Redemption, error) {
	var item pointstore.Redemption
	var rewards, results []byte
	dest := []any{&item.ID, &item.RedemptionNo, &item.AppID, &item.UserID, &item.ItemID, &item.ItemTitle, &item.Cost,
		&rewards, &results, &item.Status, &item.IntegralTransactionNo, &item.RefundTransactionNo,
		&item.RequestID, &item.Note, &item.HandledBy, &item.HandledAt, &item.DeviceID, &item.ClientIP,
		&item.CreatedAt, &item.UpdatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPointsStoreRedemptionNotFound
		}
		return nil, err
	}
	item.Rewards = []pointstore.Reward{}
	item.Results = []pointstore.RewardResult{}
	if len(rewards) > 0 {
		if err := json.Unmarshal(rewards, &item.Rewards); err != nil {
			return nil, fmt.Errorf("decode redemption rewards: %w", err)
		}
	}
	if len(results) > 0 {
		if err := json.Unmarshal(results, &item.Results); err != nil {
			return nil, fmt.Errorf("decode redemption results: %w", err)
		}
	}
	return &item, nil
}

// RedeemPointsStoreItem 兑换一件商品（单事务，要么全成要么全不成）。
//
// 顺序：锁用户行 → 幂等键回放 → 锁商品行 → 取齐事实并判定 → 扣库存 → 落兑换记录 →
// 扣积分 → 逐项发放 → 回写发放结果。
//
// 锁用户行在最前：限兑次数、积分余额、「已拥有」都是这个人的事实，不锁就有
// 「两个并发请求都读到还剩一次」的窗口。锁商品行在其次：库存是所有人共享的，
// 判定与扣减必须在同一把锁里。所有路径都按「用户 → 商品」的顺序加锁，不会互相等死。
//
// 幂等键的查询放在锁用户行之后：同一个键的两次并发请求，第二个会等第一个提交，
// 然后在这里看到第一个的记录并原样返回，而不是再扣一次。
func (r *Repository) RedeemPointsStoreItem(ctx context.Context, input pointstore.RedeemInput) (*pointstore.RedeemResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var integral, experience int64
	if err := tx.QueryRow(ctx, `SELECT integral, experience FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		input.UserID, input.AppID).Scan(&integral, &experience); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	if input.RequestID != "" {
		existing, err := scanPointsStoreRedemption(tx.QueryRow(ctx, `SELECT `+pointsStoreRedemptionColumns+`
FROM points_store_redemptions r WHERE r.appid = $1 AND r.user_id = $2 AND r.request_id = $3`,
			input.AppID, input.UserID, input.RequestID))
		if err == nil {
			return &pointstore.RedeemResult{Redemption: *existing, Balance: integral, Replayed: true}, nil
		}
		if !errors.Is(err, ErrPointsStoreRedemptionNotFound) {
			return nil, err
		}
	}

	item, err := scanPointsStoreItem(tx.QueryRow(ctx, `SELECT `+pointsStoreItemColumns+`
FROM points_store_items i WHERE i.id = $1 AND i.appid = $2 FOR UPDATE`, input.ItemID, input.AppID))
	if err != nil {
		return nil, err
	}

	entitlementFacts, err := entitlementFactsTx(ctx, tx, input.AppID, input.UserID, nil)
	if err != nil {
		return nil, err
	}
	facts, err := r.assemblePointsStoreFacts(ctx, tx, input.AppID, input.UserID, integral, experience, entitlementFacts)
	if err != nil {
		return nil, err
	}
	usage, err := pointsStoreItemUsageTx(ctx, tx, input.AppID, input.UserID, item.ID, input.Windows)
	if err != nil {
		return nil, err
	}
	// 全站名额在锁住的商品行上数：同一件商品的兑换在这里排队，数到的就是提交前的真实份数
	if item.GlobalLimited() {
		if usage.Global, err = pointsStoreItemGlobalUsageTx(ctx, tx, input.AppID, item.ID, input.Windows); err != nil {
			return nil, err
		}
	}
	clock := pointstore.Clock{Now: time.Now(), Location: input.Location, Windows: input.Windows}
	if clock.Location == nil {
		clock.Location = time.Local
	}
	// 跨进新的补货周期：先把库存补满、记下新周期，再按补满后的数判定与扣减
	if stock, rolled := item.CurrentStock(input.Windows); rolled {
		windowStart := input.Windows.Start(item.RestockPeriod)
		item.Stock, item.StockWindowStart = stock, &windowStart
		if _, err := tx.Exec(ctx, `UPDATE points_store_items SET stock = $2, stock_window_start = $3 WHERE id = $1`,
			item.ID, stock, windowStart); err != nil {
			return nil, err
		}
	}
	if availability := pointstore.Evaluate(*item, *facts, usage, clock); !availability.Redeemable {
		return nil, &PointsStoreUnavailableError{Availability: availability}
	}

	if _, err := tx.Exec(ctx, `UPDATE points_store_items
SET stock = CASE WHEN stock IS NULL THEN NULL ELSE stock - 1 END, sold_count = sold_count + 1, updated_at = NOW()
WHERE id = $1`, item.ID); err != nil {
		return nil, err
	}

	rewards := pointstore.NormalizeRewards(item.Rewards)
	status := pointstore.RedemptionCompleted
	if pointstore.HasManual(rewards) {
		status = pointstore.RedemptionPending
	}
	rewardsJSON, _ := json.Marshal(rewards)
	redemption, err := scanPointsStoreRedemption(tx.QueryRow(ctx, `INSERT INTO points_store_redemptions AS r
(redemption_no, appid, user_id, item_id, item_title, cost, rewards, status, request_id, device_id, client_ip, user_agent)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING `+pointsStoreRedemptionColumns,
		generateTransactionNo("PSR"), input.AppID, input.UserID, item.ID, item.Title, item.Cost, rewardsJSON, status,
		nullableString(input.RequestID), nullableString(input.DeviceID), nullableString(input.ClientIP),
		nullableString(truncateColumn(input.UserAgent, 256))))
	if err != nil {
		return nil, err
	}

	redemptionID := redemption.ID
	_, balance, integralTxnNo, err := applyIntegralChangeTx(ctx, tx, input.UserID, input.AppID, -item.Cost,
		"consume", pointstore.IntegralCategoryRedeem, "积分兑换", item.Title, "points_store", &redemptionID,
		map[string]any{"redemptionNo": redemption.RedemptionNo, "itemId": item.ID})
	if err != nil {
		return nil, err
	}

	results, err := r.grantPointsStoreRewardsTx(ctx, tx, redemption, rewards, facts)
	if err != nil {
		return nil, err
	}
	resultsJSON, _ := json.Marshal(results)
	if _, err := tx.Exec(ctx, `UPDATE points_store_redemptions SET results = $2, integral_transaction_no = $3
WHERE id = $1`, redemption.ID, resultsJSON, integralTxnNo); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil

	redemption.Results = results
	redemption.IntegralTransactionNo = integralTxnNo
	return &pointstore.RedeemResult{Redemption: *redemption, Balance: balance}, nil
}

// grantPointsStoreRewardsTx 在兑换事务里逐项发放。
//
// 这里的 switch 与 pointstore 的权益目录**双向钉死**（TestPointsStoreCatalogHasGrantBranch）。
// 任何一项发不出去都让整笔兑换回滚：积分退回、库存复原，不存在「扣了积分少发一样」。
func (r *Repository) grantPointsStoreRewardsTx(ctx context.Context, tx pgx.Tx, redemption *pointstore.Redemption,
	rewards []pointstore.Reward, facts *pointstore.UserFacts) ([]pointstore.RewardResult, error) {
	appID, userID := redemption.AppID, redemption.UserID
	redemptionID := redemption.ID
	meta := map[string]any{"redemptionNo": redemption.RedemptionNo}
	if redemption.ItemID != nil {
		meta["itemId"] = *redemption.ItemID
	}
	source := "积分兑换 " + redemption.ItemTitle
	out := make([]pointstore.RewardResult, 0, len(rewards))

	for _, reward := range rewards {
		described := pointstore.DescribeReward(reward, facts.Plans, facts.Features)
		result := pointstore.RewardResult{Type: reward.Type, Label: described.Label, Detail: described.Detail}

		switch reward.Type {
		case pointstore.RewardVipPlan:
			plan, err := scanVipPlan(tx.QueryRow(ctx,
				`SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1 AND id = $2`, appID, reward.RefID))
			if err != nil {
				return nil, err
			}
			if plan == nil || plan.IsTrial() {
				return nil, pointsStoreUnavailable(pointstore.ReasonRewardUnavailable)
			}
			planID := plan.ID
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID:       userID,
				AppID:        appID,
				PlanID:       &planID,
				PlanName:     plan.Name,
				Features:     plan.Features,
				DurationDays: plan.DurationDays,
				Lifetime:     plan.Lifetime,
				PayChannel:   vipdomain.ChannelPointsStore,
				PayAmount:    decimal.Zero,
				// 套餐的赠送积分不随兑换发：用积分换来的会员再返积分，等于给商品打了一个运营没算过的折扣
				Metadata: meta,
			})
			if err != nil {
				return nil, err
			}
			result.Detail = plan.Name + "（" + plan.TermLabel() + "）"
			result.TransactionNo = txn.TransactionNo

		case pointstore.RewardVipDays:
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID:       userID,
				AppID:        appID,
				PlanName:     "积分兑换",
				DurationDays: int(reward.Amount),
				PayChannel:   vipdomain.ChannelPointsStore,
				PayAmount:    decimal.Zero,
				Metadata:     meta,
			})
			if err != nil {
				return nil, err
			}
			result.TransactionNo = txn.TransactionNo

		case pointstore.RewardFeature:
			if feature, ok := facts.Features[reward.Tag]; !ok || !feature.IsActive {
				return nil, pointsStoreUnavailable(pointstore.ReasonRewardUnavailable)
			}
			unlock, err := grantFeatureUnlockTx(ctx, tx, FeatureUnlockGrant{
				AppID:     appID,
				UserID:    userID,
				Tag:       reward.Tag,
				Days:      int(reward.Amount),
				Source:    vipdomain.UnlockSourcePointsStore,
				SourceRef: redemption.RedemptionNo,
			})
			if errors.Is(err, ErrFeatureUnlockOwned) {
				return nil, pointsStoreUnavailable(pointstore.ReasonOwned)
			}
			if err != nil {
				return nil, err
			}
			result.ExpireAt = unlock.EndsAt
			result.TransactionNo = strconv.FormatInt(unlock.ID, 10)

		case pointstore.RewardExperience:
			expResult, err := r.applyExperienceChangeTx(ctx, tx, userID, appID, reward.Amount,
				pointstore.IntegralCategoryRedeem, "积分兑换经验", source, "points_store", &redemptionID, meta)
			if err != nil {
				return nil, err
			}
			if expResult.LevelChanged {
				result.Detail += "（升至 Lv." + strconv.Itoa(expResult.NewLevel) + "）"
			}
			result.TransactionNo = expResult.TransactionNo

		case pointstore.RewardBalance:
			change, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
				UserID:   userID,
				AppID:    appID,
				Type:     walletdomain.TxnTypePointsExchange,
				Amount:   reward.Money,
				Title:    "积分兑换",
				Remark:   source,
				Metadata: meta,
				// 一笔兑换天生只该入账一次，兑换单号就是最贴切的幂等键
				IdempotencyKey: "points:" + strconv.FormatInt(appID, 10) + ":" + redemption.RedemptionNo,
			})
			if err != nil {
				return nil, err
			}
			result.TransactionNo = change.Transaction.TransactionNo

		case pointstore.RewardLotteryDraws:
			balance, err := grantLotteryDrawsTx(ctx, tx, lotterydomain.TicketGrant{
				AppID: appID, UserID: userID, Amount: reward.Amount, Origin: lotterydomain.TicketOriginPointsStore,
				// 花了积分换的排在赠送的之后用；0 积分的福利商品算赠送
				Paid: redemption.Cost > 0, Ref: redemption.RedemptionNo, Title: source,
			})
			if err != nil {
				return nil, err
			}
			result.Detail += "（剩余 " + strconv.FormatInt(balance, 10) + " 次）"

		case pointstore.RewardManual:
			result.Pending = true

		default:
			// 目录里有、这里没有 —— 由测试挡在合并之前
			return nil, fmt.Errorf("points store: unsupported reward type %s", reward.Type)
		}
		out = append(out, result)
	}
	return out, nil
}

// ── 人工发放 ──

// FulfillPointsStoreRedemption 标记一笔待发放的兑换为已发放。
func (r *Repository) FulfillPointsStoreRedemption(ctx context.Context, input pointstore.HandleInput) (*pointstore.Redemption, error) {
	return r.handlePointsStoreRedemption(ctx, input, false)
}

// CancelPointsStoreRedemption 取消一笔待发放的兑换：退回积分、库存加回。
//
// 只有待发放的能取消：已到账的权益（会员、经验、余额）已经被用户用上了，
// 取消它要回答「已经用掉的怎么办」，而那不是一个按钮能回答的问题。
func (r *Repository) CancelPointsStoreRedemption(ctx context.Context, input pointstore.HandleInput) (*pointstore.Redemption, error) {
	return r.handlePointsStoreRedemption(ctx, input, true)
}

func (r *Repository) handlePointsStoreRedemption(ctx context.Context, input pointstore.HandleInput, cancel bool) (*pointstore.Redemption, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	redemption, err := scanPointsStoreRedemption(tx.QueryRow(ctx, `SELECT `+pointsStoreRedemptionColumns+`
FROM points_store_redemptions r WHERE r.id = $1 AND r.appid = $2 FOR UPDATE`, input.RedemptionID, input.AppID))
	if err != nil {
		return nil, err
	}
	if redemption.Status != pointstore.RedemptionPending {
		return nil, ErrPointsStoreRedemptionState
	}

	status := pointstore.RedemptionCompleted
	refundNo := ""
	if cancel {
		status = pointstore.RedemptionCancelled
		redemptionID := redemption.ID
		_, _, txnNo, err := applyIntegralChangeTx(ctx, tx, redemption.UserID, redemption.AppID, redemption.Cost,
			"earn", pointstore.IntegralCategoryRefund, "积分兑换退回", redemption.ItemTitle, "points_store", &redemptionID,
			map[string]any{"redemptionNo": redemption.RedemptionNo})
		if err != nil {
			return nil, err
		}
		refundNo = txnNo
		if redemption.ItemID != nil {
			// 开了补货周期时，只有本周期兑出的那份才退回库存：上个周期的货已经随补货清零，
			// 退回去会让本周期凭空多出一份
			if _, err := tx.Exec(ctx, `UPDATE points_store_items
SET stock = CASE
        WHEN stock IS NULL THEN NULL
        WHEN restock_period <> 'none' AND (stock_window_start IS NULL OR $2 < stock_window_start) THEN stock
        WHEN restock_quantity IS NOT NULL THEN LEAST(stock + 1, restock_quantity)
        ELSE stock + 1 END,
    sold_count = GREATEST(sold_count - 1, 0), updated_at = NOW()
WHERE id = $1`, *redemption.ItemID, redemption.CreatedAt); err != nil {
				return nil, err
			}
		}
	}
	for index := range redemption.Results {
		redemption.Results[index].Pending = false
	}
	resultsJSON, _ := json.Marshal(redemption.Results)

	updated, err := scanPointsStoreRedemption(tx.QueryRow(ctx, `UPDATE points_store_redemptions AS r
SET status = $3, results = $4, refund_transaction_no = $5, note = $6, handled_by = $7, handled_at = NOW(), updated_at = NOW()
WHERE r.id = $1 AND r.appid = $2
RETURNING `+pointsStoreRedemptionColumns,
		redemption.ID, redemption.AppID, status, resultsJSON, nullableString(refundNo),
		nullableString(strings.TrimSpace(input.Note)), nullableString(input.Operator)))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return updated, nil
}

// ── 查询 ──

// GetPointsStoreRedemption 取一笔兑换（带账号）。
func (r *Repository) GetPointsStoreRedemption(ctx context.Context, appID, redemptionID int64) (*pointstore.Redemption, error) {
	var account, nickname string
	item, err := scanPointsStoreRedemption(r.pool.QueryRow(ctx, `SELECT `+pointsStoreRedemptionColumns+`,
COALESCE(u.account, ''), COALESCE(p.nickname, '')
FROM points_store_redemptions r LEFT JOIN users u ON u.id = r.user_id
LEFT JOIN user_profiles p ON p.user_id = r.user_id
WHERE r.id = $1 AND r.appid = $2`, redemptionID, appID), &account, &nickname)
	if err != nil {
		return nil, err
	}
	item.Account, item.Nickname = account, nickname
	return item, nil
}

// ListPointsStoreRedemptions 兑换记录（userId / itemId / status / keyword），新的在前。
func (r *Repository) ListPointsStoreRedemptions(ctx context.Context, query pointstore.RedemptionQuery) (*pointstore.Page[pointstore.Redemption], error) {
	where := []string{"r.appid = $1"}
	args := []any{query.AppID}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		where = append(where, fmt.Sprintf("r.user_id = $%d", len(args)))
	}
	if query.ItemID > 0 {
		args = append(args, query.ItemID)
		where = append(where, fmt.Sprintf("r.item_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("r.status = $%d", len(args)))
	}
	if query.From != nil {
		args = append(args, *query.From)
		where = append(where, fmt.Sprintf("r.created_at >= $%d", len(args)))
	}
	if query.To != nil {
		args = append(args, *query.To)
		where = append(where, fmt.Sprintf("r.created_at < $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(u.account ILIKE $%d OR p.nickname ILIKE $%d OR r.redemption_no ILIKE $%d OR r.item_title ILIKE $%d)",
			len(args), len(args), len(args), len(args)))
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	from := ` FROM points_store_redemptions r LEFT JOIN users u ON u.id = r.user_id LEFT JOIN user_profiles p ON p.user_id = r.user_id`

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+pointsStoreRedemptionColumns+`, COALESCE(u.account, ''), COALESCE(p.nickname, '')`+
		from+clause+fmt.Sprintf(" ORDER BY r.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]pointstore.Redemption, 0, limit)
	for rows.Next() {
		var account, nickname string
		item, err := scanPointsStoreRedemption(rows, &account, &nickname)
		if err != nil {
			return nil, err
		}
		item.Account, item.Nickname = account, nickname
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &pointstore.Page[pointstore.Redemption]{
		Items:      items,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: int((total + int64(limit) - 1) / int64(limit)),
	}, nil
}

// PointsStoreStats 窗口内的兑换汇总、按天趋势与兑换最多的商品；待发放与上架数是当前值。
// 缺失的日期由服务层补零。
func (r *Repository) PointsStoreStats(ctx context.Context, appID int64, since, dayStart time.Time, timezone string) (*pointstore.Stats, error) {
	stats := &pointstore.Stats{Trend: []pointstore.StatsDay{}, TopItems: []pointstore.StatsItem{}}
	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE status <> 'cancelled' AND created_at >= $2),
  COUNT(DISTINCT user_id) FILTER (WHERE status <> 'cancelled' AND created_at >= $2),
  COALESCE(SUM(cost) FILTER (WHERE status <> 'cancelled' AND created_at >= $2), 0),
  COUNT(*) FILTER (WHERE status = 'pending'),
  COUNT(*) FILTER (WHERE status <> 'cancelled' AND created_at >= $3),
  COUNT(*) FILTER (WHERE status = 'cancelled' AND created_at >= $2)
FROM points_store_redemptions WHERE appid = $1`, appID, since, dayStart).Scan(
		&stats.Summary.Redemptions, &stats.Summary.Redeemers, &stats.Summary.PointsSpent,
		&stats.Summary.Pending, &stats.Summary.Today, &stats.Summary.Cancelled); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM points_store_items
WHERE appid = $1 AND status = 'active' AND (ends_at IS NULL OR ends_at > NOW())`, appID).
		Scan(&stats.Summary.ActiveItems); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
       COUNT(*), COALESCE(SUM(cost), 0), COUNT(DISTINCT user_id)
FROM points_store_redemptions
WHERE appid = $1 AND created_at >= $2 AND status <> 'cancelled'
GROUP BY day ORDER BY day`, appID, since, timezone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day pointstore.StatsDay
		if err := rows.Scan(&day.Date, &day.Redemptions, &day.PointsSpent, &day.Redeemers); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Trend = append(stats.Trend, day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	topRows, err := r.pool.Query(ctx, `SELECT item_id, MAX(item_title), COUNT(*), COALESCE(SUM(cost), 0)
FROM points_store_redemptions
WHERE appid = $1 AND created_at >= $2 AND status <> 'cancelled'
GROUP BY item_id ORDER BY COUNT(*) DESC, MAX(id) DESC LIMIT 8`, appID, since)
	if err != nil {
		return nil, err
	}
	defer topRows.Close()
	for topRows.Next() {
		var item pointstore.StatsItem
		if err := topRows.Scan(&item.ItemID, &item.Title, &item.Redemptions, &item.PointsSpent); err != nil {
			return nil, err
		}
		stats.TopItems = append(stats.TopItems, item)
	}
	return stats, topRows.Err()
}
