package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	vipdomain "aegis/internal/domain/vip"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

// 试用期会员的资格账本。
//
// 「会员判定」与「试用发放」都收在这个文件里，因为它们读的是同一批事实：
// users.vip_expire_at（还是不是会员）、vip_trial_claims（领没领过、领到什么时候）、
// vip_transactions（凭什么是会员）。分开取就必然出现两次查询之间状态变了的窗口。

var (
	// ErrTrialAlreadyClaimed 该用户已经领过试用（一人一次由唯一约束保证）
	ErrTrialAlreadyClaimed = errors.New("postgres: vip trial already claimed")
	// ErrTrialMemberActive 当前已是会员，领试用只会把到期时间再往后推
	ErrTrialMemberActive = errors.New("postgres: vip trial rejected, member active")
	// ErrTrialDeviceClaimed 同一设备已领过（仅在设备维度去重开启时）
	ErrTrialDeviceClaimed = errors.New("postgres: vip trial already claimed on this device")
)

const vipTrialClaimColumns = `id, appid, user_id, plan_id, plan_name, duration_days, trial_ends_at,
transaction_no, COALESCE(device_id, ''), device_locked, COALESCE(client_ip, ''), COALESCE(operator, ''), created_at`

// activeFeatureUnionSQL 当前生效的功能标识：**尚未到期**的每一段开通所带功能的并集。
//
// 取并集而不是「最近一次开通那份」：会员期是顺延的，先买基础版再买高级版时两段
// 都还没到期，用户理所当然认为两边的功能现在都能用。已经用完的那几段
// （expire_after 已过）自然落在集合之外，权益随时间自己收敛，不需要任何清理任务。
//
// 逐行 UNNEST 而不是 `array_agg(features)`：后者要求所有数组维度一致，
// 两个套餐功能数不同时 Postgres 会直接报 "cannot accumulate arrays of different dimensionality"。
const activeFeatureUnionSQL = `ARRAY(
    SELECT DISTINCT tag FROM vip_transactions vt, UNNEST(vt.features) AS tag
    WHERE vt.appid = u.appid AND vt.user_id = u.id AND vt.expire_after > NOW()
)`

// GetActiveTrialPlan 当前启用中的试用套餐。
//
// 唯一部分索引保证每个应用至多一条，因此这里不需要（也不应该）用 ORDER BY 挑一条 ——
// 靠排序决定"领哪个"意味着改一次 sort_order 就换了发放内容。
func (r *Repository) GetActiveTrialPlan(ctx context.Context, appID int64) (*vipdomain.Plan, error) {
	return scanVipPlan(r.pool.QueryRow(ctx, `SELECT `+vipPlanColumns+
		` FROM vip_plans WHERE appid = $1 AND kind = 'trial' AND is_active LIMIT 1`, appID))
}

// GetVipTrialClaim 该用户的试用领取记录，没领过返回 nil。
func (r *Repository) GetVipTrialClaim(ctx context.Context, appID int64, userID int64) (*vipdomain.TrialClaim, error) {
	return scanVipTrialClaim(r.pool.QueryRow(ctx, `SELECT `+vipTrialClaimColumns+
		` FROM vip_trial_claims WHERE appid = $1 AND user_id = $2 LIMIT 1`, appID, userID))
}

// TrialDeviceClaimed 这台设备是否已经有人领过试用。
//
// 刻意不限定 device_locked：设备维度是"从开启之后生效"还是"追溯既往"，
// 只有后者防得住"先在规则关着时领一轮、开启后换个号再领"。
func (r *Repository) TrialDeviceClaimed(ctx context.Context, appID int64, deviceID string, exceptUserID int64) (bool, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return false, nil
	}
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM vip_trial_claims WHERE appid = $1 AND device_id = $2 AND user_id <> $3)`,
		appID, deviceID, exceptUserID).Scan(&exists)
	return exists, err
}

// GetVipEntitlementFacts 一次取齐会员判定所需的全部事实。
//
// 一次查询而不是三次：三次之间用户可能刚好买了会员 / 试用刚好到期，
// 拼出来的结论会自相矛盾（比如同时"不是会员"和"试用中"）。
func (r *Repository) GetVipEntitlementFacts(ctx context.Context, appID int64, userID int64) (*vipdomain.EvalInput, error) {
	var (
		facts        vipdomain.EvalInput
		lastChannel  *string
		lastPlanName *string
		claimID      *int64
	)
	claim := vipdomain.TrialClaim{AppID: appID, UserID: userID}
	err := r.pool.QueryRow(ctx, `SELECT u.vip_expire_at,
       t.pay_channel, t.plan_name,
       c.id, c.plan_id, c.plan_name, c.duration_days, c.trial_ends_at, c.transaction_no,
       COALESCE(c.device_id, ''), COALESCE(c.device_locked, FALSE), c.created_at,
       ` + activeFeatureUnionSQL + `
FROM users u
LEFT JOIN vip_trial_claims c ON c.appid = u.appid AND c.user_id = u.id
LEFT JOIN LATERAL (
    SELECT pay_channel, plan_name FROM vip_transactions
    WHERE appid = u.appid AND user_id = u.id ORDER BY id DESC LIMIT 1
) t ON TRUE
WHERE u.id = $1 AND u.appid = $2 LIMIT 1`, userID, appID).
		Scan(&facts.ExpireAt, &lastChannel, &lastPlanName,
			&claimID, &claim.PlanID, &claim.PlanName, &claim.DurationDays, &claim.TrialEndsAt,
			&claim.TransactionNo, &claim.DeviceID, &claim.DeviceLocked, &claim.CreatedAt,
			&facts.Features)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	if lastChannel != nil {
		facts.LastChannel = *lastChannel
	}
	if lastPlanName != nil {
		facts.LastPlanName = *lastPlanName
	}
	if claimID != nil {
		claim.ID = *claimID
		facts.Claim = &claim
	}
	return &facts, nil
}

// TrialClaimInput 一次试用领取指令。
type TrialClaimInput struct {
	AppID    int64
	UserID   int64
	Plan     vipdomain.Plan
	DeviceID string
	ClientIP string
	// Operator 管理员代领时留痕，用户自助领取为空
	Operator string
}

// ClaimVipTrial 领取试用（单事务）：锁用户 → 查资格 → 续期并记账 → 落资格记录。
//
// 三道闸门的顺序不能换：先看有没有领过（说得出"已经领过"比说"你已经是会员"准确），
// 再看是不是已经是会员，最后才是设备维度。用户行锁让同一个人的并发请求串行化，
// 唯一约束兜住不同人同设备的并发。
func (r *Repository) ClaimVipTrial(ctx context.Context, input TrialClaimInput) (*vipdomain.TrialClaimResult, error) {
	if input.Plan.DurationDays <= 0 {
		return nil, fmt.Errorf("vip trial duration days must be positive")
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

	var expireBefore *time.Time
	if err := tx.QueryRow(ctx, `SELECT vip_expire_at FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		input.UserID, input.AppID).Scan(&expireBefore); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	now := time.Now().UTC()

	existing, err := scanVipTrialClaim(tx.QueryRow(ctx, `SELECT `+vipTrialClaimColumns+
		` FROM vip_trial_claims WHERE appid = $1 AND user_id = $2 LIMIT 1`, input.AppID, input.UserID))
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// 还在这段试用期内 ⇒ 把上一次的结果原样返回。
		// 领取接口没有幂等键（它天然一人一次），而"第一次成功了但响应没回到客户端"
		// 是最常见的一次重试 —— 那次重试不该看到一句"你已经领过了"的报错。
		if now.Before(existing.TrialEndsAt) {
			facts, err := entitlementFactsTx(ctx, tx, input.AppID, input.UserID, existing)
			if err != nil {
				return nil, err
			}
			// 试用套餐就在手上，别让结论里的 trialOffer 说成「未开放试用」
			facts.TrialPlan = input.Plan.TrialRef()
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			tx = nil
			return &vipdomain.TrialClaimResult{
				Claim:       *existing,
				Entitlement: vipdomain.Evaluate(*facts, time.Now()),
				Replayed:    true,
			}, nil
		}
		return nil, ErrTrialAlreadyClaimed
	}

	if expireBefore != nil && expireBefore.After(now) {
		return nil, ErrTrialMemberActive
	}

	deviceID := strings.TrimSpace(input.DeviceID)
	if input.Plan.TrialDeviceLimited && deviceID != "" {
		var taken bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM vip_trial_claims WHERE appid = $1 AND device_id = $2 AND user_id <> $3)`,
			input.AppID, deviceID, input.UserID).Scan(&taken); err != nil {
			return nil, err
		}
		if taken {
			return nil, ErrTrialDeviceClaimed
		}
	}

	planID := input.Plan.ID
	txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
		UserID:        input.UserID,
		AppID:         input.AppID,
		PlanID:        &planID,
		PlanName:      input.Plan.Name,
		Features:      input.Plan.Features,
		DurationDays:  input.Plan.DurationDays,
		PayChannel:    vipdomain.ChannelTrial,
		PayAmount:     decimal.Zero,
		BonusIntegral: input.Plan.BonusIntegral,
		Operator:      input.Operator,
		Metadata: map[string]any{
			"trial":    true,
			"planId":   input.Plan.ID,
			"deviceId": deviceID,
			"clientIp": strings.TrimSpace(input.ClientIP),
		},
	})
	if err != nil {
		return nil, err
	}

	claim, err := scanVipTrialClaim(tx.QueryRow(ctx,
		`INSERT INTO vip_trial_claims (appid, user_id, plan_id, plan_name, duration_days, trial_ends_at,
transaction_no, device_id, device_locked, client_ip, operator, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
RETURNING `+vipTrialClaimColumns,
		input.AppID, input.UserID, planID, input.Plan.Name, input.Plan.DurationDays, txn.ExpireAfter,
		txn.TransactionNo, nullableString(deviceID), input.Plan.TrialDeviceLimited,
		nullableString(strings.TrimSpace(input.ClientIP)), nullableString(input.Operator)))
	if err != nil {
		// 并发落到唯一约束上：按撞的是哪个约束翻译，两者的补救动作完全不同
		// （自己已经领过 ⇒ 去看状态；设备被占 ⇒ 换设备或找客服）。
		switch uniqueConstraintName(err) {
		case "uq_vip_trial_claims_user":
			return nil, ErrTrialAlreadyClaimed
		case "uq_vip_trial_claims_device":
			return nil, ErrTrialDeviceClaimed
		}
		return nil, err
	}

	facts, err := entitlementFactsTx(ctx, tx, input.AppID, input.UserID, claim)
	if err != nil {
		return nil, err
	}
	facts.TrialPlan = input.Plan.TrialRef()
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil

	return &vipdomain.TrialClaimResult{
		Claim:       *claim,
		Transaction: txn,
		Entitlement: vipdomain.Evaluate(*facts, time.Now()),
	}, nil
}

// entitlementFactsTx 事务内取判定事实（试用记录由调用方给出，避免重复查一次）。
func entitlementFactsTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64, claim *vipdomain.TrialClaim) (*vipdomain.EvalInput, error) {
	facts := vipdomain.EvalInput{Claim: claim}
	var lastChannel, lastPlanName *string
	if err := tx.QueryRow(ctx, `SELECT u.vip_expire_at, t.pay_channel, t.plan_name, `+activeFeatureUnionSQL+`
FROM users u
LEFT JOIN LATERAL (
    SELECT pay_channel, plan_name FROM vip_transactions
    WHERE appid = u.appid AND user_id = u.id ORDER BY id DESC LIMIT 1
) t ON TRUE
WHERE u.id = $1 AND u.appid = $2 LIMIT 1`, userID, appID).
		Scan(&facts.ExpireAt, &lastChannel, &lastPlanName, &facts.Features); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	if lastChannel != nil {
		facts.LastChannel = *lastChannel
	}
	if lastPlanName != nil {
		facts.LastPlanName = *lastPlanName
	}
	return &facts, nil
}

// ListVipTrialClaims 管理端：试用领取记录 + 汇总。
//
// 汇总里的 converted 是开试用的**唯一理由**：领了之后有没有付费。
// 没有这一列，这张表只是一堆"谁在什么时候领了七天"的流水。
func (r *Repository) ListVipTrialClaims(ctx context.Context, appID int64, page int, limit int) ([]vipdomain.TrialClaim, int64, vipdomain.TrialSummary, error) {
	var summary vipdomain.TrialSummary
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*),
       COUNT(*) FILTER (WHERE c.trial_ends_at > NOW()),
       COUNT(*) FILTER (WHERE EXISTS (
           SELECT 1 FROM vip_transactions t
           WHERE t.appid = c.appid AND t.user_id = c.user_id
             AND t.pay_channel IN ('wallet', 'payment_order')
             AND t.created_at >= c.created_at))
FROM vip_trial_claims c WHERE c.appid = $1`, appID).
		Scan(&summary.Total, &summary.Active, &summary.Converted); err != nil {
		return nil, 0, summary, err
	}
	if summary.Total == 0 {
		return []vipdomain.TrialClaim{}, 0, summary, nil
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `SELECT c.id, c.appid, c.user_id, c.plan_id, c.plan_name, c.duration_days,
       c.trial_ends_at, c.transaction_no, COALESCE(c.device_id, ''), c.device_locked,
       COALESCE(c.client_ip, ''), COALESCE(c.operator, ''), c.created_at,
       COALESCE(u.account, ''),
       EXISTS (SELECT 1 FROM vip_transactions t
               WHERE t.appid = c.appid AND t.user_id = c.user_id
                 AND t.pay_channel IN ('wallet', 'payment_order')
                 AND t.created_at >= c.created_at) AS converted
FROM vip_trial_claims c
LEFT JOIN users u ON u.id = c.user_id
WHERE c.appid = $1
ORDER BY c.id DESC LIMIT $2 OFFSET $3`, appID, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, summary, err
	}
	defer rows.Close()

	items := make([]vipdomain.TrialClaim, 0, limit)
	for rows.Next() {
		var item vipdomain.TrialClaim
		if err := rows.Scan(&item.ID, &item.AppID, &item.UserID, &item.PlanID, &item.PlanName,
			&item.DurationDays, &item.TrialEndsAt, &item.TransactionNo, &item.DeviceID, &item.DeviceLocked,
			&item.ClientIP, &item.Operator, &item.CreatedAt, &item.Account, &item.Converted); err != nil {
			return nil, 0, summary, err
		}
		items = append(items, item)
	}
	return items, summary.Total, summary, rows.Err()
}

// DeleteVipTrialClaim 清除某用户的试用领取记录（恢复资格）。
//
// **只删资格，不收回已发放的会员时长** —— 那是两件事：资格是"还能不能领"，
// 时长是"已经给出去的东西"。要收回时长走退款冲正或管理端调整，
// 在这里顺手扣掉会让客服的一次善意操作变成用户眼里的"会员没了"。
func (r *Repository) DeleteVipTrialClaim(ctx context.Context, appID int64, userID int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM vip_trial_claims WHERE appid = $1 AND user_id = $2`, appID, userID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func scanVipTrialClaim(row interface{ Scan(dest ...any) error }) (*vipdomain.TrialClaim, error) {
	var claim vipdomain.TrialClaim
	if err := row.Scan(&claim.ID, &claim.AppID, &claim.UserID, &claim.PlanID, &claim.PlanName,
		&claim.DurationDays, &claim.TrialEndsAt, &claim.TransactionNo, &claim.DeviceID,
		&claim.DeviceLocked, &claim.ClientIP, &claim.Operator, &claim.CreatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &claim, nil
}

// uniqueConstraintName 唯一冲突撞的是哪个约束，非唯一冲突返回空串。
func uniqueConstraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}
