package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"aegis/internal/domain/coupon"
	"aegis/internal/domain/itemgrant"
)

// 优惠系统：模板、用户券、活动（docs/coupons.md）。订单上的预占、结算、释放与退款在 coupon_order_repository.go。
//
// 锁序：users → coupon_promotions → user_coupons → coupon_templates → payment_orders → payment_order_promotions。
// 发券（grantCouponTx）只在已经锁住用户行的事务里调用，或自己先锁用户行。

var (
	ErrCouponTemplateNotFound  = errors.New("coupon template not found")
	ErrCouponPromotionNotFound = errors.New("coupon promotion not found")
	ErrUserCouponNotFound      = errors.New("user coupon not found")
	// ErrCouponRuleLocked 已发出券的模板不能改规则字段
	ErrCouponRuleLocked = errors.New("coupon template rule locked")
	// ErrCouponInUse 已发出券的模板、已有订单参与的活动不能删除
	ErrCouponInUse = errors.New("coupon template or promotion in use")
	// ErrCouponStatusTransition 状态不能这样改
	ErrCouponStatusTransition = errors.New("coupon status transition not allowed")
	// ErrUserCouponState 用户券当前状态不能做这个操作
	ErrUserCouponState = errors.New("user coupon state")
	// ErrCouponStockShrink 发放总量不能小于已发数量
	ErrCouponStockShrink = errors.New("coupon stock below issued")
)

// CouponUnavailableError 发券 / 领券失败的具体原因（能直接给用户看）。
type CouponUnavailableError struct {
	Code   string
	Reason string
}

func (e *CouponUnavailableError) Error() string { return e.Reason }

func couponUnavailable(code, reason string) error {
	return &CouponUnavailableError{Code: code, Reason: reason}
}

const couponTemplateColumns = `id, appid, name, description, theme, kind, amount::text, rate, max_discount::text, min_amount::text,
scopes, plan_ids, stack_with_promotion, validity_mode, valid_from, valid_until, valid_days, valid_delay_days,
total_quantity, issued_count, used_count, discount_total::text, per_user_limit, claimable, claim_starts_at, claim_ends_at,
audience, refund_return, status, COALESCE(created_by, ''), COALESCE(updated_by, ''), created_at, updated_at`

func scanCouponTemplate(row pgx.Row) (*coupon.Template, error) {
	var (
		t                                    coupon.Template
		amount, maxDiscount, minAmount, disc string
		audience                             []byte
	)
	if err := row.Scan(&t.ID, &t.AppID, &t.Name, &t.Description, &t.Theme, &t.Kind, &amount, &t.Rate, &maxDiscount, &minAmount,
		&t.Scopes, &t.PlanIDs, &t.StackWithPromotion, &t.ValidityMode, &t.ValidFrom, &t.ValidUntil, &t.ValidDays, &t.ValidDelayDays,
		&t.TotalQuantity, &t.IssuedCount, &t.UsedCount, &disc, &t.PerUserLimit, &t.Claimable, &t.ClaimStartsAt, &t.ClaimEndsAt,
		&audience, &t.RefundReturn, &t.Status, &t.CreatedBy, &t.UpdatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Amount, t.MaxDiscount, t.MinAmount, t.DiscountTotal = decimal.RequireFromString(amount), decimal.RequireFromString(maxDiscount),
		decimal.RequireFromString(minAmount), decimal.RequireFromString(disc)
	_ = json.Unmarshal(audience, &t.Audience)
	if t.Scopes == nil {
		t.Scopes = []string{}
	}
	if t.PlanIDs == nil {
		t.PlanIDs = []int64{}
	}
	return &t, nil
}

func collectCouponTemplates(rows pgx.Rows) ([]coupon.Template, error) {
	defer rows.Close()
	out := []coupon.Template{}
	for rows.Next() {
		t, err := scanCouponTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func couponPageBounds(page, limit, max int) (int, int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > max {
		limit = max
	}
	return page, limit, (page - 1) * limit
}

// ── 模板 ──

// CreateCouponTemplate 新建模板（草稿）。
func (r *Repository) CreateCouponTemplate(ctx context.Context, in coupon.TemplateInput) (*coupon.Template, error) {
	audience, _ := json.Marshal(in.Audience)
	return scanCouponTemplate(r.pool.QueryRow(ctx, `INSERT INTO coupon_templates (appid, name, description, theme, kind, amount, rate, max_discount, min_amount,
  scopes, plan_ids, stack_with_promotion, validity_mode, valid_from, valid_until, valid_days, valid_delay_days,
  total_quantity, per_user_limit, claimable, claim_starts_at, claim_ends_at, audience, refund_return, status, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,'draft',$25,$25)
RETURNING `+couponTemplateColumns,
		in.AppID, in.Name, in.Description, in.Theme, in.Kind, in.Amount.StringFixed(2), in.Rate, in.MaxDiscount.StringFixed(2), in.MinAmount.StringFixed(2),
		in.Scopes, in.PlanIDs, in.StackWithPromotion, in.ValidityMode, in.ValidFrom, in.ValidUntil, in.ValidDays, in.ValidDelayDays,
		in.TotalQuantity, in.PerUserLimit, in.Claimable, in.ClaimStartsAt, in.ClaimEndsAt, audience, in.RefundReturn, nullableString(in.Operator)))
}

// UpdateCouponTemplate 修改模板。已发出券的只能改名称、说明、色调、库存、领取设置与退款返还。
func (r *Repository) UpdateCouponTemplate(ctx context.Context, id int64, in coupon.TemplateInput) (*coupon.Template, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := scanCouponTemplate(tx.QueryRow(ctx, `SELECT `+couponTemplateColumns+` FROM coupon_templates WHERE id = $1 AND appid = $2 FOR UPDATE`, id, in.AppID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCouponTemplateNotFound
	}
	if err != nil {
		return nil, err
	}
	if old.IssuedCount > 0 && !coupon.RuleLocked(*old, in) {
		return nil, ErrCouponRuleLocked
	}
	if in.TotalQuantity > 0 && in.TotalQuantity < old.IssuedCount {
		return nil, ErrCouponStockShrink
	}
	audience, _ := json.Marshal(in.Audience)
	t, err := scanCouponTemplate(tx.QueryRow(ctx, `UPDATE coupon_templates SET name = $3, description = $4, theme = $5, kind = $6, amount = $7, rate = $8,
  max_discount = $9, min_amount = $10, scopes = $11, plan_ids = $12, stack_with_promotion = $13, validity_mode = $14, valid_from = $15,
  valid_until = $16, valid_days = $17, valid_delay_days = $18, total_quantity = $19, per_user_limit = $20, claimable = $21,
  claim_starts_at = $22, claim_ends_at = $23, audience = $24, refund_return = $25, updated_by = $26, updated_at = NOW()
WHERE id = $1 AND appid = $2
RETURNING `+couponTemplateColumns,
		id, in.AppID, in.Name, in.Description, in.Theme, in.Kind, in.Amount.StringFixed(2), in.Rate, in.MaxDiscount.StringFixed(2), in.MinAmount.StringFixed(2),
		in.Scopes, in.PlanIDs, in.StackWithPromotion, in.ValidityMode, in.ValidFrom, in.ValidUntil, in.ValidDays, in.ValidDelayDays,
		in.TotalQuantity, in.PerUserLimit, in.Claimable, in.ClaimStartsAt, in.ClaimEndsAt, audience, in.RefundReturn, nullableString(in.Operator)))
	if err != nil {
		return nil, err
	}
	return t, tx.Commit(ctx)
}

// GetCouponTemplate 取一个模板。
func (r *Repository) GetCouponTemplate(ctx context.Context, appID, id int64) (*coupon.Template, error) {
	t, err := scanCouponTemplate(r.pool.QueryRow(ctx, `SELECT `+couponTemplateColumns+` FROM coupon_templates WHERE id = $1 AND appid = $2`, id, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCouponTemplateNotFound
	}
	return t, err
}

// GetCouponTemplatesByIDs 按编号批量取模板（不存在的不返回）。
func (r *Repository) GetCouponTemplatesByIDs(ctx context.Context, appID int64, ids []int64) (map[int64]coupon.Template, error) {
	out := map[int64]coupon.Template{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+couponTemplateColumns+` FROM coupon_templates WHERE appid = $1 AND id = ANY($2)`, appID, ids)
	if err != nil {
		return nil, err
	}
	items, err := collectCouponTemplates(rows)
	if err != nil {
		return nil, err
	}
	for _, t := range items {
		out[t.ID] = t
	}
	return out, nil
}

// ListCouponTemplates 模板列表（新的在前）。
func (r *Repository) ListCouponTemplates(ctx context.Context, q coupon.TemplateQuery) (coupon.Page[coupon.Template], error) {
	page, limit, offset := couponPageBounds(q.Page, q.Limit, 200)
	where := `WHERE appid = $1`
	args := []any{q.AppID}
	if q.Status != "" {
		args = append(args, q.Status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		args = append(args, "%"+kw+"%")
		where += fmt.Sprintf(" AND (name ILIKE $%d OR description ILIKE $%d)", len(args), len(args))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM coupon_templates `+where, args...).Scan(&total); err != nil {
		return coupon.Page[coupon.Template]{}, err
	}
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, `SELECT `+couponTemplateColumns+` FROM coupon_templates `+where+
		fmt.Sprintf(` ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'paused' THEN 1 WHEN 'draft' THEN 2 ELSE 3 END, id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return coupon.Page[coupon.Template]{}, err
	}
	items, err := collectCouponTemplates(rows)
	if err != nil {
		return coupon.Page[coupon.Template]{}, err
	}
	return coupon.NewPage(items, total, page, limit), nil
}

// ListClaimableCouponTemplates 领券中心：进行中、开放领取、领取期没结束的模板。
func (r *Repository) ListClaimableCouponTemplates(ctx context.Context, appID int64) ([]coupon.Template, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+couponTemplateColumns+` FROM coupon_templates
WHERE appid = $1 AND status = 'active' AND claimable AND (claim_ends_at IS NULL OR claim_ends_at > NOW())
  AND (validity_mode <> 'fixed' OR valid_until > NOW())
ORDER BY id DESC LIMIT 200`, appID)
	if err != nil {
		return nil, err
	}
	return collectCouponTemplates(rows)
}

// SetCouponTemplateStatus 改模板状态。
func (r *Repository) SetCouponTemplateStatus(ctx context.Context, appID, id int64, status, operator string) (*coupon.Template, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current string
	if err := tx.QueryRow(ctx, `SELECT status FROM coupon_templates WHERE id = $1 AND appid = $2 FOR UPDATE`, id, appID).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCouponTemplateNotFound
		}
		return nil, err
	}
	if !coupon.CanTransition(current, status) {
		return nil, ErrCouponStatusTransition
	}
	t, err := scanCouponTemplate(tx.QueryRow(ctx, `UPDATE coupon_templates SET status = $3, updated_by = $4, updated_at = NOW()
WHERE id = $1 AND appid = $2 RETURNING `+couponTemplateColumns, id, appID, status, nullableString(operator)))
	if err != nil {
		return nil, err
	}
	return t, tx.Commit(ctx)
}

// DeleteCouponTemplate 删除模板：只能删一张都没发出去的。
func (r *Repository) DeleteCouponTemplate(ctx context.Context, appID, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM coupon_templates WHERE id = $1 AND appid = $2 AND issued_count = 0
  AND NOT EXISTS (SELECT 1 FROM user_coupons WHERE template_id = $1)`, id, appID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetCouponTemplate(ctx, appID, id); err != nil {
			return err
		}
		return ErrCouponInUse
	}
	return nil
}

// ── 发券 ──

const couponCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCouponCode() string {
	var b strings.Builder
	b.Grow(14)
	b.WriteString("C")
	limit := big.NewInt(int64(len(couponCodeAlphabet)))
	for i := 0; i < 12; i++ {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			// crypto/rand 失败时退回时间戳拼接，宁可不够随机也不能发不出券
			b.WriteByte(couponCodeAlphabet[(time.Now().UnixNano()>>i)%int64(len(couponCodeAlphabet))])
			continue
		}
		b.WriteByte(couponCodeAlphabet[n.Int64()])
	}
	return b.String()
}

// grantCouponTx 各渠道发券的唯一入口：条件扣库存（全部渠道共用 total_quantity）→ 逐张写入用户券。
//
// 不判领取资格（领券中心的人群、每人限领在 ClaimCoupon 里判），只判模板此刻能不能发（状态、固定有效期是否已过、库存）。
// 调用方负责事务与用户行锁；用户必须属于这个应用。不能发时返回 *CouponUnavailableError。
func grantCouponTx(ctx context.Context, tx pgx.Tx, in coupon.GrantInput) (*coupon.GrantResult, error) {
	if in.Count < 1 {
		in.Count = 1
	}
	if in.Count > coupon.MaxCouponGrantCount*25 {
		return nil, couponUnavailable(coupon.ReasonLimit, "一次发券太多")
	}
	var belongs bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND appid = $2)`, in.UserID, in.AppID).Scan(&belongs); err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrUserNotFound
	}
	statuses := []string{coupon.StatusActive}
	if in.IgnoreStatus {
		statuses = append(statuses, coupon.StatusPaused)
	}
	t, err := scanCouponTemplate(tx.QueryRow(ctx, `UPDATE coupon_templates SET issued_count = issued_count + $3, updated_at = NOW()
WHERE id = $1 AND appid = $2 AND status = ANY($4) AND (total_quantity = 0 OR issued_count + $3 <= total_quantity)
  AND (validity_mode <> 'fixed' OR valid_until > NOW())
RETURNING `+couponTemplateColumns, in.TemplateID, in.AppID, in.Count, statuses))
	if errors.Is(err, pgx.ErrNoRows) {
		current, getErr := scanCouponTemplate(tx.QueryRow(ctx, `SELECT `+couponTemplateColumns+` FROM coupon_templates WHERE id = $1 AND appid = $2`, in.TemplateID, in.AppID))
		if errors.Is(getErr, pgx.ErrNoRows) {
			return nil, couponUnavailable(coupon.ReasonStatus, fmt.Sprintf("优惠券 #%d 不存在", in.TemplateID))
		}
		if getErr != nil {
			return nil, getErr
		}
		if reason := coupon.Grantable(*current, in.Count, in.IgnoreStatus, time.Now()); reason != nil {
			code := coupon.ReasonStatus
			if current.TotalQuantity > 0 && current.IssuedCount+int64(in.Count) > current.TotalQuantity {
				code = coupon.ReasonSoldOut
			}
			return nil, couponUnavailable(code, reason.Error())
		}
		return nil, couponUnavailable(coupon.ReasonStatus, "「"+current.Name+"」现在不能发放")
	}
	if err != nil {
		return nil, err
	}
	now := time.Now()
	from, until := coupon.CouponWindow(*t, now)
	result := &coupon.GrantResult{TemplateName: t.Name, Coupons: make([]coupon.UserCoupon, 0, in.Count)}
	for i := 0; i < in.Count; i++ {
		var c coupon.UserCoupon
		var discount string
		inserted := false
		for attempt := 0; attempt < 5 && !inserted; attempt++ {
			err := tx.QueryRow(ctx, `INSERT INTO user_coupons (appid, user_id, template_id, code, status, source, source_ref, valid_from, valid_until)
VALUES ($1, $2, $3, $4, 'available', $5, $6, $7, $8)
ON CONFLICT (appid, code) DO NOTHING
RETURNING id, appid, user_id, template_id, code, status, source, source_ref, valid_from, valid_until, discount::text, created_at, updated_at`,
				in.AppID, in.UserID, t.ID, newCouponCode(), in.Source, couponTruncate(in.SourceRef, 96), from, until).
				Scan(&c.ID, &c.AppID, &c.UserID, &c.TemplateID, &c.Code, &c.Status, &c.Source, &c.SourceRef, &c.ValidFrom, &c.ValidUntil, &discount, &c.CreatedAt, &c.UpdatedAt)
			if errors.Is(err, pgx.ErrNoRows) {
				continue // 券号撞了，换一个
			}
			if err != nil {
				return nil, err
			}
			inserted = true
		}
		if !inserted {
			return nil, fmt.Errorf("coupon code collision")
		}
		c.Discount = decimal.RequireFromString(discount)
		tc := *t
		c.Template = &tc
		result.Coupons = append(result.Coupons, c)
	}
	return result, nil
}

func couponTruncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max]
}

// GrantCoupons 单独发券（管理员发放等）：自己开事务、锁用户行。
func (r *Repository) GrantCoupons(ctx context.Context, in coupon.GrantInput) (*coupon.GrantResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := couponLockUserTx(ctx, tx, in.AppID, in.UserID); err != nil {
		return nil, err
	}
	result, err := grantCouponTx(ctx, tx, in)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}

func couponLockUserTx(ctx context.Context, tx pgx.Tx, appID, userID int64) error {
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}
	return nil
}

// ClaimCoupon 领券中心领一张：锁用户 → 读模板 → 数已领张数 → 判资格 → 发券（同一事务）。
func (r *Repository) ClaimCoupon(ctx context.Context, appID, userID, templateID int64, facts coupon.UserFacts) (*coupon.UserCoupon, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := couponLockUserTx(ctx, tx, appID, userID); err != nil {
		return nil, err
	}
	t, err := scanCouponTemplate(tx.QueryRow(ctx, `SELECT `+couponTemplateColumns+` FROM coupon_templates WHERE id = $1 AND appid = $2`, templateID, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCouponTemplateNotFound
	}
	if err != nil {
		return nil, err
	}
	var claimed int64
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM user_coupons WHERE template_id = $1 AND user_id = $2 AND source = 'claim'`,
		templateID, userID).Scan(&claimed); err != nil {
		return nil, err
	}
	if code, reason := coupon.ClaimCheck(*t, claimed, facts, time.Now()); code != "" {
		return nil, couponUnavailable(code, reason)
	}
	result, err := grantCouponTx(ctx, tx, coupon.GrantInput{AppID: appID, UserID: userID, TemplateID: templateID, Count: 1, Source: coupon.SourceClaim})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	c := result.Coupons[0]
	return &c, nil
}

// CountCouponClaims 这个人在领券中心各模板已领几张。
func (r *Repository) CountCouponClaims(ctx context.Context, appID, userID int64, templateIDs []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(templateIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT template_id, COUNT(*) FROM user_coupons
WHERE appid = $1 AND user_id = $2 AND source = 'claim' AND template_id = ANY($3) GROUP BY template_id`, appID, userID, templateIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ── 用户券 ──

const userCouponColumns = `uc.id, uc.appid, uc.user_id, uc.template_id, uc.code, uc.status, uc.source, uc.source_ref, uc.valid_from, uc.valid_until,
COALESCE(uc.order_no, ''), uc.used_at, uc.discount::text, COALESCE(uc.revoked_reason, ''), uc.created_at, uc.updated_at`

func scanUserCouponWithTemplate(row pgx.Row, withUser bool) (*coupon.UserCoupon, error) {
	var (
		c        coupon.UserCoupon
		discount string
		t        coupon.Template
		amount   string
		maxD     string
		minA     string
		disc     string
		audience []byte
	)
	dest := []any{&c.ID, &c.AppID, &c.UserID, &c.TemplateID, &c.Code, &c.Status, &c.Source, &c.SourceRef, &c.ValidFrom, &c.ValidUntil,
		&c.OrderNo, &c.UsedAt, &discount, &c.RevokedReason, &c.CreatedAt, &c.UpdatedAt,
		&t.ID, &t.AppID, &t.Name, &t.Description, &t.Theme, &t.Kind, &amount, &t.Rate, &maxD, &minA,
		&t.Scopes, &t.PlanIDs, &t.StackWithPromotion, &t.ValidityMode, &t.ValidFrom, &t.ValidUntil, &t.ValidDays, &t.ValidDelayDays,
		&t.TotalQuantity, &t.IssuedCount, &t.UsedCount, &disc, &t.PerUserLimit, &t.Claimable, &t.ClaimStartsAt, &t.ClaimEndsAt,
		&audience, &t.RefundReturn, &t.Status, &t.CreatedBy, &t.UpdatedBy, &t.CreatedAt, &t.UpdatedAt}
	if withUser {
		dest = append(dest, &c.UserAccount, &c.UserNickname)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	c.Discount = decimal.RequireFromString(discount)
	t.Amount, t.MaxDiscount, t.MinAmount, t.DiscountTotal = decimal.RequireFromString(amount), decimal.RequireFromString(maxD),
		decimal.RequireFromString(minA), decimal.RequireFromString(disc)
	_ = json.Unmarshal(audience, &t.Audience)
	c.Template = &t
	return &c, nil
}

func couponPrefixed(columns, prefix string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "COALESCE("):
			parts[i] = "COALESCE(" + prefix + "." + strings.TrimPrefix(part, "COALESCE(")
		case strings.HasPrefix(part, "''"), strings.HasPrefix(part, "'"):
			parts[i] = part
		default:
			parts[i] = prefix + "." + part
		}
	}
	return strings.Join(parts, ", ")
}

var couponTemplateColumnsT = couponPrefixed(couponTemplateColumns, "t")

// ListUserCoupons 用户券列表：用户端（UserID 必填）与管理端共用。
func (r *Repository) ListUserCoupons(ctx context.Context, q coupon.UserCouponQuery, withUser bool) (coupon.Page[coupon.UserCoupon], error) {
	page, limit, offset := couponPageBounds(q.Page, q.Limit, 200)
	where := `WHERE uc.appid = $1`
	args := []any{q.AppID}
	add := func(clause string, value any) {
		args = append(args, value)
		where += fmt.Sprintf(clause, len(args))
	}
	if q.UserID > 0 {
		add(" AND uc.user_id = $%d", q.UserID)
	}
	if q.TemplateID > 0 {
		add(" AND uc.template_id = $%d", q.TemplateID)
	}
	if q.Source != "" {
		add(" AND uc.source = $%d", q.Source)
	}
	switch q.Status {
	case "":
	case "usable":
		where += " AND uc.status IN ('available', 'locked') AND uc.valid_until > NOW()"
	case coupon.CouponAvailable:
		where += " AND uc.status = 'available' AND uc.valid_until > NOW() AND uc.valid_from <= NOW()"
	case coupon.CouponNotStarted:
		where += " AND uc.status = 'available' AND uc.valid_from > NOW()"
	case coupon.CouponExpired:
		where += " AND (uc.status = 'expired' OR (uc.status = 'available' AND uc.valid_until <= NOW()))"
	default:
		add(" AND uc.status = $%d", q.Status)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		args = append(args, "%"+kw+"%")
		n := len(args)
		where += fmt.Sprintf(" AND (uc.code ILIKE $%d OR uc.order_no ILIKE $%d OR t.name ILIKE $%d", n, n, n)
		if withUser {
			where += fmt.Sprintf(" OR u.account ILIKE $%d OR COALESCE(up.nickname, '') ILIKE $%d", n, n)
		}
		where += ")"
	}
	from := ` FROM user_coupons uc JOIN coupon_templates t ON t.id = uc.template_id`
	if withUser {
		from += ` JOIN users u ON u.id = uc.user_id LEFT JOIN user_profiles up ON up.user_id = u.id`
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+` `+where, args...).Scan(&total); err != nil {
		return coupon.Page[coupon.UserCoupon]{}, err
	}
	columns := userCouponColumns + ", " + couponTemplateColumnsT
	if withUser {
		columns += ", u.account, COALESCE(up.nickname, '')"
	}
	order := ` ORDER BY uc.id DESC`
	if q.UserID > 0 && (q.Status == "usable" || q.Status == coupon.CouponAvailable) {
		order = ` ORDER BY uc.valid_until ASC, uc.id ASC`
	}
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, `SELECT `+columns+from+` `+where+order+fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return coupon.Page[coupon.UserCoupon]{}, err
	}
	defer rows.Close()
	items := []coupon.UserCoupon{}
	for rows.Next() {
		c, err := scanUserCouponWithTemplate(rows, withUser)
		if err != nil {
			return coupon.Page[coupon.UserCoupon]{}, err
		}
		items = append(items, *c)
	}
	if err := rows.Err(); err != nil {
		return coupon.Page[coupon.UserCoupon]{}, err
	}
	return coupon.NewPage(items, total, page, limit), nil
}

// ListQuoteCoupons 试算用的券：可用与被占用、还没过期的，最多 200 张（先过期的在前）。
func (r *Repository) ListQuoteCoupons(ctx context.Context, appID, userID int64) ([]coupon.UserCoupon, error) {
	page, err := r.ListUserCoupons(ctx, coupon.UserCouponQuery{AppID: appID, UserID: userID, Status: "usable", Limit: 200}, false)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// GetUserCoupon 取一张券（管理端；userID>0 时只取这个人的）。
func (r *Repository) GetUserCoupon(ctx context.Context, appID, userID, id int64) (*coupon.UserCoupon, error) {
	query := `SELECT ` + userCouponColumns + `, ` + couponTemplateColumnsT + ` FROM user_coupons uc JOIN coupon_templates t ON t.id = uc.template_id
WHERE uc.appid = $1 AND uc.id = $2`
	args := []any{appID, id}
	if userID > 0 {
		query += ` AND uc.user_id = $3`
		args = append(args, userID)
	}
	c, err := scanUserCouponWithTemplate(r.pool.QueryRow(ctx, query, args...), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserCouponNotFound
	}
	return c, err
}

// CouponSummary 这个人的券按展示状态计数。
func (r *Repository) CouponSummary(ctx context.Context, appID, userID int64) (map[string]int64, error) {
	out := map[string]int64{coupon.CouponAvailable: 0, coupon.CouponNotStarted: 0, coupon.CouponLocked: 0,
		coupon.CouponUsed: 0, coupon.CouponExpired: 0, coupon.CouponRevoked: 0}
	rows, err := r.pool.Query(ctx, `SELECT CASE
  WHEN status = 'available' AND valid_until <= NOW() THEN 'expired'
  WHEN status = 'available' AND valid_from > NOW() THEN 'not_started'
  ELSE status END AS s, COUNT(*)
FROM user_coupons WHERE appid = $1 AND user_id = $2 GROUP BY s`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
}

// RevokeUserCoupon 管理员收回一张券：只能收回可用的（被占用、已使用的不行）。
func (r *Repository) RevokeUserCoupon(ctx context.Context, appID, id int64, reason string) (*coupon.UserCoupon, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE user_coupons SET status = 'revoked', revoked_reason = $3, updated_at = NOW()
WHERE id = $1 AND appid = $2 AND status IN ('available', 'expired')`, id, appID, nullableString(couponTruncate(reason, 200)))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetUserCoupon(ctx, appID, 0, id); err != nil {
			return nil, err
		}
		return nil, ErrUserCouponState
	}
	return r.GetUserCoupon(ctx, appID, 0, id)
}

// ExpireUserCoupons 把过了有效期的可用券标成已过期（读取时本来就按时间推导，这里是为了列表与统计口径一致）。
func (r *Repository) ExpireUserCoupons(ctx context.Context, limit int) (int64, error) {
	if limit < 1 {
		limit = 5000
	}
	tag, err := r.pool.Exec(ctx, `UPDATE user_coupons SET status = 'expired', updated_at = NOW()
WHERE id IN (SELECT id FROM user_coupons WHERE status = 'available' AND valid_until <= NOW() ORDER BY valid_until LIMIT $1 FOR UPDATE SKIP LOCKED)`, limit)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 活动 ──

const couponPromotionColumns = `id, appid, name, description, tag, theme, kind, rule, priority, scopes, plan_ids, package_ids, audience,
stack_with_coupon, starts_at, ends_at, per_user_limit, total_limit, budget::text, status, used_count, discount_total::text,
COALESCE(created_by, ''), COALESCE(updated_by, ''), created_at, updated_at`

func scanCouponPromotion(row pgx.Row) (*coupon.Promotion, error) {
	var (
		p              coupon.Promotion
		rule, audience []byte
		budget, disc   string
	)
	if err := row.Scan(&p.ID, &p.AppID, &p.Name, &p.Description, &p.Tag, &p.Theme, &p.Kind, &rule, &p.Priority, &p.Scopes, &p.PlanIDs, &p.PackageIDs,
		&audience, &p.StackWithCoupon, &p.StartsAt, &p.EndsAt, &p.PerUserLimit, &p.TotalLimit, &budget, &p.Status, &p.UsedCount, &disc,
		&p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.Rule = coupon.PromotionRule{Amount: decimal.Zero, MaxDiscount: decimal.Zero, MinAmount: decimal.Zero}
	_ = json.Unmarshal(rule, &p.Rule)
	_ = json.Unmarshal(audience, &p.Audience)
	p.Budget, p.DiscountTotal = decimal.RequireFromString(budget), decimal.RequireFromString(disc)
	if p.Scopes == nil {
		p.Scopes = []string{}
	}
	if p.PlanIDs == nil {
		p.PlanIDs = []int64{}
	}
	if p.PackageIDs == nil {
		p.PackageIDs = []int64{}
	}
	return &p, nil
}

func collectCouponPromotions(rows pgx.Rows) ([]coupon.Promotion, error) {
	defer rows.Close()
	out := []coupon.Promotion{}
	for rows.Next() {
		p, err := scanCouponPromotion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// CreateCouponPromotion 新建活动（草稿）。
func (r *Repository) CreateCouponPromotion(ctx context.Context, in coupon.PromotionInput) (*coupon.Promotion, error) {
	rule, _ := json.Marshal(in.Rule)
	audience, _ := json.Marshal(in.Audience)
	return scanCouponPromotion(r.pool.QueryRow(ctx, `INSERT INTO coupon_promotions (appid, name, description, tag, theme, kind, rule, priority,
  scopes, plan_ids, package_ids, audience, stack_with_coupon, starts_at, ends_at, per_user_limit, total_limit, budget, status, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,'draft',$19,$19)
RETURNING `+couponPromotionColumns,
		in.AppID, in.Name, in.Description, in.Tag, in.Theme, in.Kind, rule, in.Priority, in.Scopes, in.PlanIDs, in.PackageIDs, audience,
		in.StackWithCoupon, in.StartsAt, in.EndsAt, in.PerUserLimit, in.TotalLimit, in.Budget.StringFixed(2), nullableString(in.Operator)))
}

// UpdateCouponPromotion 修改活动。已有订单参与过的活动不能改类型（改规则对之后的订单生效，已下的单按下单时的价格）。
func (r *Repository) UpdateCouponPromotion(ctx context.Context, id int64, in coupon.PromotionInput) (*coupon.Promotion, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := scanCouponPromotion(tx.QueryRow(ctx, `SELECT `+couponPromotionColumns+` FROM coupon_promotions WHERE id = $1 AND appid = $2 FOR UPDATE`, id, in.AppID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCouponPromotionNotFound
	}
	if err != nil {
		return nil, err
	}
	if old.UsedCount > 0 && old.Kind != in.Kind {
		return nil, ErrCouponRuleLocked
	}
	rule, _ := json.Marshal(in.Rule)
	audience, _ := json.Marshal(in.Audience)
	p, err := scanCouponPromotion(tx.QueryRow(ctx, `UPDATE coupon_promotions SET name = $3, description = $4, tag = $5, theme = $6, kind = $7,
  rule = $8, priority = $9, scopes = $10, plan_ids = $11, package_ids = $12, audience = $13, stack_with_coupon = $14, starts_at = $15,
  ends_at = $16, per_user_limit = $17, total_limit = $18, budget = $19, updated_by = $20, updated_at = NOW()
WHERE id = $1 AND appid = $2 RETURNING `+couponPromotionColumns,
		id, in.AppID, in.Name, in.Description, in.Tag, in.Theme, in.Kind, rule, in.Priority, in.Scopes, in.PlanIDs, in.PackageIDs, audience,
		in.StackWithCoupon, in.StartsAt, in.EndsAt, in.PerUserLimit, in.TotalLimit, in.Budget.StringFixed(2), nullableString(in.Operator)))
	if err != nil {
		return nil, err
	}
	return p, tx.Commit(ctx)
}

// GetCouponPromotion 取一期活动。
func (r *Repository) GetCouponPromotion(ctx context.Context, appID, id int64) (*coupon.Promotion, error) {
	p, err := scanCouponPromotion(r.pool.QueryRow(ctx, `SELECT `+couponPromotionColumns+` FROM coupon_promotions WHERE id = $1 AND appid = $2`, id, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCouponPromotionNotFound
	}
	return p, err
}

// ListCouponPromotions 活动列表。
func (r *Repository) ListCouponPromotions(ctx context.Context, q coupon.PromotionQuery) (coupon.Page[coupon.Promotion], error) {
	page, limit, offset := couponPageBounds(q.Page, q.Limit, 200)
	where := `WHERE appid = $1`
	args := []any{q.AppID}
	if q.Kind != "" {
		args = append(args, q.Kind)
		where += fmt.Sprintf(" AND kind = $%d", len(args))
	}
	if q.Status != "" {
		args = append(args, q.Status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		args = append(args, "%"+kw+"%")
		where += fmt.Sprintf(" AND (name ILIKE $%d OR description ILIKE $%d OR tag ILIKE $%d)", len(args), len(args), len(args))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM coupon_promotions `+where, args...).Scan(&total); err != nil {
		return coupon.Page[coupon.Promotion]{}, err
	}
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, `SELECT `+couponPromotionColumns+` FROM coupon_promotions `+where+
		fmt.Sprintf(` ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'paused' THEN 1 WHEN 'draft' THEN 2 ELSE 3 END, priority DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return coupon.Page[coupon.Promotion]{}, err
	}
	items, err := collectCouponPromotions(rows)
	if err != nil {
		return coupon.Page[coupon.Promotion]{}, err
	}
	return coupon.NewPage(items, total, page, limit), nil
}

// ListLiveCouponPromotions 进行中、还没结束的活动（定价用；没开始的也带上，用户端可以预告）。
func (r *Repository) ListLiveCouponPromotions(ctx context.Context, appID int64) ([]coupon.Promotion, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+couponPromotionColumns+` FROM coupon_promotions
WHERE appid = $1 AND status = 'active' AND (ends_at IS NULL OR ends_at > NOW())
ORDER BY priority DESC, id ASC LIMIT 100`, appID)
	if err != nil {
		return nil, err
	}
	return collectCouponPromotions(rows)
}

// SetCouponPromotionStatus 改活动状态。
func (r *Repository) SetCouponPromotionStatus(ctx context.Context, appID, id int64, status, operator string) (*coupon.Promotion, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current string
	if err := tx.QueryRow(ctx, `SELECT status FROM coupon_promotions WHERE id = $1 AND appid = $2 FOR UPDATE`, id, appID).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCouponPromotionNotFound
		}
		return nil, err
	}
	if !coupon.CanTransition(current, status) {
		return nil, ErrCouponStatusTransition
	}
	p, err := scanCouponPromotion(tx.QueryRow(ctx, `UPDATE coupon_promotions SET status = $3, updated_by = $4, updated_at = NOW()
WHERE id = $1 AND appid = $2 RETURNING `+couponPromotionColumns, id, appID, status, nullableString(operator)))
	if err != nil {
		return nil, err
	}
	return p, tx.Commit(ctx)
}

// DeleteCouponPromotion 删除活动：只能删没有任何订单参与过的。
func (r *Repository) DeleteCouponPromotion(ctx context.Context, appID, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM coupon_promotions WHERE id = $1 AND appid = $2 AND used_count = 0
  AND NOT EXISTS (SELECT 1 FROM payment_order_promotions WHERE promotion_id = $1 OR rebate_promotion_id = $1)`, id, appID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetCouponPromotion(ctx, appID, id); err != nil {
			return err
		}
		return ErrCouponInUse
	}
	return nil
}

// CountPromotionUsage 这个人参与过各活动几次（预占中与已结算的，按价格活动与返券活动分别数）。
func (r *Repository) CountPromotionUsage(ctx context.Context, appID, userID int64, ids []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id, COUNT(*) FROM (
  SELECT promotion_id AS id FROM payment_order_promotions
  WHERE appid = $1 AND user_id = $2 AND promotion_id = ANY($3) AND status IN ('reserved', 'settled')
  UNION ALL
  SELECT rebate_promotion_id FROM payment_order_promotions
  WHERE appid = $1 AND user_id = $2 AND rebate_promotion_id = ANY($3) AND status IN ('reserved', 'settled')
) x GROUP BY id`, appID, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// CouponUserFacts 资格判定用的用户事实：注册时间、付过款的订单数、会员到期时间是否还没到。
// 永久会员与按档位的会员由服务层用 GetVipHoldings 补判。
func (r *Repository) CouponUserFacts(ctx context.Context, appID, userID int64) (coupon.UserFacts, error) {
	var facts coupon.UserFacts
	err := r.pool.QueryRow(ctx, `SELECT u.created_at, COALESCE(u.vip_expire_at > NOW(), FALSE),
  (SELECT COUNT(*) FROM payment_orders o WHERE o.appid = u.appid AND o.user_id = u.id
     AND (o.status = 'paid' OR (o.status = 'pending' AND o.expire_at > NOW())))
FROM users u WHERE u.id = $1 AND u.appid = $2`, userID, appID).Scan(&facts.RegisteredAt, &facts.IsVip, &facts.PaidOrders)
	if errors.Is(err, pgx.ErrNoRows) {
		return facts, ErrUserNotFound
	}
	return facts, err
}

// grantCouponRewardTx 各奖励渠道发优惠券的统一入口：返回一句结果说明（「满 100 减 20 2 张」）。
// 券发不出去（不存在、已结束、发完）时返回 *CouponUnavailableError，由各渠道决定是整笔回滚还是记失败。
func grantCouponRewardTx(ctx context.Context, tx pgx.Tx, appID, userID, templateID, count int64, source, ref string) (string, []int64, error) {
	if count < 1 {
		count = 1
	}
	granted, err := grantCouponTx(ctx, tx, coupon.GrantInput{
		AppID: appID, UserID: userID, TemplateID: templateID, Count: int(count), Source: source, SourceRef: ref,
	})
	if err != nil {
		return "", nil, err
	}
	ids := make([]int64, 0, len(granted.Coupons))
	for _, c := range granted.Coupons {
		ids = append(ids, c.ID)
	}
	return fmt.Sprintf("%s %d 张", granted.TemplateName, count), ids, nil
}

// CouponRefs 应用的券目录：编号 → 是否发得出去（进行中或暂停中）与名称。
func (r *Repository) CouponRefs(ctx context.Context, appID int64) (map[int64]itemgrant.CouponRef, error) {
	return couponRefsQ(ctx, r.pool, appID)
}

func couponRefsQ(ctx context.Context, q queryExecutor, appID int64) (map[int64]itemgrant.CouponRef, error) {
	rows, err := q.Query(ctx, `SELECT id, name, status IN ('active', 'paused') FROM coupon_templates WHERE appid = $1`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]itemgrant.CouponRef{}
	for rows.Next() {
		var ref itemgrant.CouponRef
		if err := rows.Scan(&ref.ID, &ref.Name, &ref.Live); err != nil {
			return nil, err
		}
		out[ref.ID] = ref
	}
	return out, rows.Err()
}
