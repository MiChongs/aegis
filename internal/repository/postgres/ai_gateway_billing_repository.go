package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"aegis/internal/domain/aigateway"
	walletdomain "aegis/internal/domain/wallet"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

// AI 中转网关的计费：暂扣 → 结算 / 放回 → 退款，以及赠送额度。
//
// 三条不变式贯穿这个文件：
//
//  1. **失败不扣费**。暂扣不写任何流水：积分从 users.integral 移出、余额移进 user_wallets.frozen、
//     额度记 reserved、赠送额度移进 credit_frozen。失败时原样放回，积分与余额的流水里什么都不留。
//  2. **一笔成功调用只写一条消费流水**（积分、余额、赠送额度各至多一条），金额是实际用量的价格。
//     结算先把暂扣放回再扣实际金额，两步在同一个事务里，流水上看到的就是「余额 → 余额 − 价格」。
//  3. **结算永不超过暂扣能覆盖的范围**：暂扣按估算向上取整，结算加零头向下取整（证明见
//     aigateway.TestChargeWithCarryNeverExceedsHold）。实际用量超出暂扣时按扣费顺序从实时余额补，
//     补不上的记 shortfall —— 只会少收，不会把任何余额扣成负数。
//
// 锁序与全库一致：users → ai_gateway_accounts → ai_gateway_keys → ai_gateway_quota_usage → user_wallets。
// 同一用户的网关事务都先锁 users 行，彼此串行；其它模块也是 users → user_wallets，不成环。

var (
	ErrAIGatewayAudience            = errors.New("ai gateway: members only")
	ErrAIGatewayVipOnlyModel        = errors.New("ai gateway: model is members only")
	ErrAIGatewayRateLimited         = errors.New("ai gateway: rate limited")
	ErrAIGatewayConcurrency         = errors.New("ai gateway: too many concurrent requests")
	ErrAIGatewayDailyLimit          = errors.New("ai gateway: daily spend limit reached")
	ErrAIGatewayKeySpendLimit       = errors.New("ai gateway: key spend limit reached")
	ErrAIGatewayKeyUnusable         = errors.New("ai gateway: key disabled or expired")
	ErrAIGatewayNotRefundable       = errors.New("ai gateway: request not refundable")
	ErrAIGatewayCreditInsufficient  = errors.New("ai gateway: credit insufficient")
	ErrAIGatewayCreditRequestReused = errors.New("ai gateway: credit request id reused")
)

// AIGatewayInsufficientError 余额不够付这次调用（Need 是估算价格，Available 是全部来源合计）。
type AIGatewayInsufficientError struct {
	Need      int64
	Available int64
}

func (e *AIGatewayInsufficientError) Error() string {
	return fmt.Sprintf("ai gateway: insufficient balance (need %d, available %d)", e.Need, e.Available)
}

// AIGatewayIdempotencyError 幂等键已被一次进行中或已成功的调用占用。
type AIGatewayIdempotencyError struct {
	RequestNo string
	Status    string
}

func (e *AIGatewayIdempotencyError) Error() string {
	return "ai gateway: idempotency key in use by " + e.RequestNo
}

// AIGatewayIntegralCategory / AIGatewayIntegralRefundCategory 网关写积分流水的分类（已在排行榜目录登记为不计入）。
const (
	AIGatewayIntegralCategory       = "ai_gateway"
	AIGatewayIntegralRefundCategory = "ai_gateway_refund"
)

// ── 锁与账户 ──

// aiGatewayLedger 一次网关事务锁住的用户侧状态。
type aiGatewayLedger struct {
	tx        pgx.Tx
	appID     int64
	userID    int64
	integral  int64
	vipExpire *time.Time
	lifetime  *time.Time
	account   aigateway.Account

	walletLocked bool
	walletCents  int64
}

// lockAIGatewayUserTx 锁用户行与网关账户行（账户行不存在时先建）。
func lockAIGatewayUserTx(ctx context.Context, tx pgx.Tx, appID, userID int64) (*aiGatewayLedger, error) {
	ledger := &aiGatewayLedger{tx: tx, appID: appID, userID: userID}
	if err := tx.QueryRow(ctx, `SELECT integral, vip_expire_at, vip_lifetime_at FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		userID, appID).Scan(&ledger.integral, &ledger.vipExpire, &ledger.lifetime); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ai_gateway_accounts (user_id, appid) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING`,
		userID, appID); err != nil {
		return nil, err
	}
	account := &ledger.account
	account.AppID, account.UserID = appID, userID
	if err := tx.QueryRow(ctx, `SELECT credit, credit_frozen, integral_carry, wallet_carry, total_spent, total_requests
FROM ai_gateway_accounts WHERE user_id = $1 FOR UPDATE`, userID).Scan(&account.Credit, &account.CreditFrozen,
		&account.IntegralCarry, &account.WalletCarry, &account.TotalSpent, &account.TotalRequests); err != nil {
		return nil, err
	}
	return ledger, nil
}

// lockWallet 锁钱包行（不存在时惰性建行，与 applyWalletChangeTx 同一做法）。
func (l *aiGatewayLedger) lockWallet(ctx context.Context) error {
	if l.walletLocked {
		return nil
	}
	if _, err := l.tx.Exec(ctx, `INSERT INTO user_wallets (user_id, appid) SELECT id, appid FROM users WHERE id = $1 AND appid = $2
ON CONFLICT (user_id) DO NOTHING`, l.userID, l.appID); err != nil {
		return err
	}
	var balance string
	if err := l.tx.QueryRow(ctx, `SELECT balance::text FROM user_wallets WHERE user_id = $1 AND appid = $2 FOR UPDATE`,
		l.userID, l.appID).Scan(&balance); err != nil {
		return err
	}
	l.walletCents = decimal.RequireFromString(balance).Shift(2).Floor().IntPart()
	l.walletLocked = true
	return nil
}

// moveIntegral 暂扣（负）或放回（正）积分，不写流水。
func (l *aiGatewayLedger) moveIntegral(ctx context.Context, delta int64) error {
	if delta == 0 {
		return nil
	}
	if l.integral+delta < 0 {
		return fmt.Errorf("ai gateway: integral hold would go negative (balance %d, delta %d)", l.integral, delta)
	}
	if _, err := l.tx.Exec(ctx, `UPDATE users SET integral = integral + $3, updated_at = NOW() WHERE id = $1 AND appid = $2`,
		l.userID, l.appID, delta); err != nil {
		return err
	}
	l.integral += delta
	return nil
}

// freezeWallet 余额 → 冻结（cents 为正）或冻结 → 余额（为负），不写流水。
func (l *aiGatewayLedger) freezeWallet(ctx context.Context, cents int64) error {
	if cents == 0 {
		return nil
	}
	if !l.walletLocked {
		return errors.New("ai gateway: wallet not locked")
	}
	if l.walletCents-cents < 0 {
		return fmt.Errorf("ai gateway: wallet hold would go negative (balance %d, hold %d)", l.walletCents, cents)
	}
	// 分桶：暂扣先冻赠送余额（与正式扣费「先扣赠送」一致），放回时先把冻住的赠送余额还回去。
	// UPDATE 右侧读的都是旧值，两列在同一条语句里此消彼长，不变量
	// bonus_balance ≤ balance、frozen_bonus ≤ frozen 在每一步都成立。
	if cents > 0 {
		amount := decimal.New(cents, -2).StringFixed(2)
		if _, err := l.tx.Exec(ctx, `UPDATE user_wallets SET balance = balance - $3::numeric, frozen = frozen + $3::numeric,
bonus_balance = bonus_balance - LEAST(bonus_balance, $3::numeric),
frozen_bonus = frozen_bonus + LEAST(bonus_balance, $3::numeric),
updated_at = NOW() WHERE user_id = $1 AND appid = $2`, l.userID, l.appID, amount); err != nil {
			return err
		}
	} else {
		amount := decimal.New(-cents, -2).StringFixed(2)
		if _, err := l.tx.Exec(ctx, `UPDATE user_wallets SET balance = balance + $3::numeric, frozen = frozen - $3::numeric,
bonus_balance = bonus_balance + LEAST(frozen_bonus, $3::numeric),
frozen_bonus = frozen_bonus - LEAST(frozen_bonus, $3::numeric),
updated_at = NOW() WHERE user_id = $1 AND appid = $2`, l.userID, l.appID, amount); err != nil {
			return err
		}
	}
	l.walletCents -= cents
	return nil
}

func (l *aiGatewayLedger) saveAccount(ctx context.Context) error {
	a := l.account
	_, err := l.tx.Exec(ctx, `UPDATE ai_gateway_accounts SET credit = $2, credit_frozen = $3, integral_carry = $4,
wallet_carry = $5, total_spent = $6, total_requests = $7, updated_at = NOW() WHERE user_id = $1`,
		l.userID, a.Credit, a.CreditFrozen, a.IntegralCarry, a.WalletCarry, a.TotalSpent, a.TotalRequests)
	return err
}

func (l *aiGatewayLedger) insertCredit(ctx context.Context, kind string, amount int64, refType, refID, remark, operator, requestID string) error {
	_, err := l.tx.Exec(ctx, `INSERT INTO ai_gateway_credit_ledger
(appid, user_id, kind, amount, balance_after, ref_type, ref_id, remark, operator, request_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		l.appID, l.userID, kind, amount, l.account.Credit, refType, refID, truncateColumn(remark, 200),
		nullableString(operator), nullableString(requestID))
	return err
}

// lockAIGatewayQuotaTx 锁住一个周期额度行（不存在时先建），返回已用与暂扣。
func lockAIGatewayQuotaTx(ctx context.Context, tx pgx.Tx, appID, userID int64, source, period string) (int64, int64, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO ai_gateway_quota_usage (user_id, appid, source, period_key) VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, source, period_key) DO NOTHING`, userID, appID, source, period); err != nil {
		return 0, 0, err
	}
	var used, reserved int64
	err := tx.QueryRow(ctx, `SELECT used, reserved FROM ai_gateway_quota_usage
WHERE user_id = $1 AND source = $2 AND period_key = $3 FOR UPDATE`, userID, source, period).Scan(&used, &reserved)
	return used, reserved, err
}

// adjustAIGatewayQuotaTx 调整周期额度的已用与暂扣。GREATEST 只是兜底：正常路径上放回的量恰好等于暂扣的量。
func adjustAIGatewayQuotaTx(ctx context.Context, tx pgx.Tx, appID, userID int64, source, period string, usedDelta, reservedDelta int64) error {
	if usedDelta == 0 && reservedDelta == 0 {
		return nil
	}
	if _, _, err := lockAIGatewayQuotaTx(ctx, tx, appID, userID, source, period); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE ai_gateway_quota_usage
SET used = GREATEST(used + $4, 0), reserved = GREATEST(reserved + $5, 0), updated_at = NOW()
WHERE user_id = $1 AND source = $2 AND period_key = $3`, userID, source, period, usedDelta, reservedDelta)
	return err
}

func isPeriodicSource(source string) bool {
	meta, ok := aigateway.FindSource(source)
	return ok && meta.Periodic
}

func saturatingMul(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > math.MaxInt64/b {
		return math.MaxInt64
	}
	return a * b
}

func saturatingAdd(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

// lockAIGatewayRequestTx 锁住一条调用记录。
func lockAIGatewayRequestTx(ctx context.Context, tx pgx.Tx, requestNo string, appID int64) (*aigateway.Request, error) {
	query := `SELECT ` + aiGatewayRequestColumns + ` FROM ai_gateway_requests r WHERE r.request_no = $1`
	args := []any{requestNo}
	if appID > 0 {
		query += ` AND r.appid = $2`
		args = append(args, appID)
	}
	return scanAIGatewayRequest(tx.QueryRow(ctx, query+` FOR UPDATE`, args...))
}

// lockAIGatewayKeyTx 锁密钥行（已删除的也锁：结算要把它名下的暂扣放掉）。
func lockAIGatewayKeyTx(ctx context.Context, tx pgx.Tx, keyID int64) error {
	if keyID <= 0 {
		return nil
	}
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM ai_gateway_keys WHERE id = $1 FOR UPDATE`, keyID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func hasHoldSource(holds []aigateway.Hold, source string) bool {
	return slices.ContainsFunc(holds, func(h aigateway.Hold) bool { return h.Source == source })
}

// ── 暂扣 ──

// ReserveAIGatewayRequest 校验能否调用并暂扣估算价格，写下 reserved 状态的调用记录。
//
// 余额不够整笔估算时：对话（Clampable）把输出上限压到付得起的数量（至少 MinOutputTokens），
// 其余接口直接拒绝。
func (r *Repository) ReserveAIGatewayRequest(ctx context.Context, in aigateway.ReserveInput) (*aigateway.ReserveResult, error) {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	settings := in.Settings
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
	profile := aigateway.Profile{IsVip: isVipAt(ledger.vipExpire, ledger.lifetime, now), Level: 1}
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT current_level FROM user_level_records WHERE user_id = $1 AND appid = $2), 1)`,
		in.UserID, in.AppID).Scan(&profile.Level); err != nil {
		return nil, err
	}
	if settings.Audience == aigateway.AudienceVip && !profile.IsVip {
		return nil, ErrAIGatewayAudience
	}
	if in.Model.VipOnly && !profile.IsVip {
		return nil, ErrAIGatewayVipOnlyModel
	}

	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey != "" {
		var existingNo, existingStatus string
		err := tx.QueryRow(ctx, `SELECT request_no, status FROM ai_gateway_requests
WHERE user_id = $1 AND idempotency_key = $2 AND status NOT IN ('failed', 'expired') LIMIT 1`, in.UserID, idempotencyKey).
			Scan(&existingNo, &existingStatus)
		if err == nil {
			return nil, &AIGatewayIdempotencyError{RequestNo: existingNo, Status: existingStatus}
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	// 并发与频率在锁住用户行之后数：同一用户的暂扣彼此串行，数出来的就是准数。
	if settings.MaxConcurrent > 0 {
		var inFlight int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_requests
WHERE user_id = $1 AND status = 'reserved' AND expires_at > $2`, in.UserID, now).Scan(&inFlight); err != nil {
			return nil, err
		}
		if inFlight >= settings.MaxConcurrent {
			return nil, ErrAIGatewayConcurrency
		}
	}
	if settings.RPM > 0 {
		var recent int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_requests
WHERE appid = $1 AND user_id = $2 AND created_at > $3`, in.AppID, in.UserID, now.Add(-time.Minute)).Scan(&recent); err != nil {
			return nil, err
		}
		if recent >= settings.RPM {
			return nil, ErrAIGatewayRateLimited
		}
	}

	keyRemaining := int64(math.MaxInt64)
	var keyID int64
	keyHint := ""
	if in.Key != nil {
		keyID, keyHint = in.Key.ID, in.Key.Hint
		var current aigateway.Key
		err := tx.QueryRow(ctx, `SELECT status, expires_at, spend_limit, spent, reserved FROM ai_gateway_keys
WHERE id = $1 AND user_id = $2 FOR UPDATE`, keyID, in.UserID).Scan(&current.Status, &current.ExpiresAt,
			&current.SpendLimitMicro, &current.SpentMicro, &current.ReservedMicro)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAIGatewayKeyUnusable
		}
		if err != nil {
			return nil, err
		}
		if !current.Usable(now) {
			return nil, ErrAIGatewayKeyUnusable
		}
		if current.SpendLimitMicro > 0 {
			keyRemaining = max(current.SpendLimitMicro-current.SpentMicro-current.ReservedMicro, 0)
		}
	}

	factor := settings.Factor(profile)
	estimate := aigateway.CostMicro(in.Model.Price, in.Estimate, factor)
	loc := settings.Location()
	snapshot := aigateway.BillingSnapshot{
		Sources:    slices.Clone(settings.Sources),
		WalletRate: settings.WalletRate,
		Timezone:   settings.Timezone,
		Quotas:     map[string]int64{},
		Profile:    profile,
	}

	available := make([]aigateway.Availability, 0, len(settings.Sources))
	var totalAvailable int64
	for _, source := range settings.Sources {
		item := aigateway.Availability{Source: source}
		switch source {
		case aigateway.SourceFreeDaily, aigateway.SourceVipDaily, aigateway.SourceVipMonthly:
			meta, _ := aigateway.FindSource(source)
			limit := settings.QuotaMicro(source)
			if limit <= 0 || (meta.VipOnly && !profile.IsVip) {
				continue
			}
			snapshot.Quotas[source] = limit
			item.Period = aigateway.PeriodKey(source, now, loc)
			used, reserved, err := lockAIGatewayQuotaTx(ctx, tx, in.AppID, in.UserID, source, item.Period)
			if err != nil {
				return nil, err
			}
			item.Micro = max(limit-used-reserved, 0)
		case aigateway.SourceCredit:
			item.Micro = ledger.account.Credit
		case aigateway.SourceIntegral:
			item.Micro = saturatingMul(ledger.integral, aigateway.MicroPerPoint)
		case aigateway.SourceWallet:
			if err := ledger.lockWallet(ctx); err != nil {
				return nil, err
			}
			item.Micro = aigateway.WalletAvailableMicro(ledger.walletCents, settings.WalletRate)
		default:
			continue
		}
		available = append(available, item)
		totalAvailable = saturatingAdd(totalAvailable, item.Micro)
	}

	spendRemaining := int64(math.MaxInt64)
	if limit := settings.QuotaMicro(aigateway.SpendDaily); limit > 0 {
		snapshot.SpendLimit = limit
		snapshot.SpendPeriod = aigateway.PeriodKey(aigateway.SpendDaily, now, loc)
		used, reserved, err := lockAIGatewayQuotaTx(ctx, tx, in.AppID, in.UserID, aigateway.SpendDaily, snapshot.SpendPeriod)
		if err != nil {
			return nil, err
		}
		spendRemaining = max(limit-used-reserved, 0)
	}

	budget := min(totalAvailable, keyRemaining, spendRemaining)
	need := estimate
	clamp := 0
	if need > budget {
		if in.Clampable {
			maxOutput := aigateway.MaxAffordableOutput(in.Model.Price, in.Estimate, factor, budget)
			if maxOutput >= aigateway.MinOutputTokens && maxOutput < in.Estimate.OutputTokens {
				clamped := in.Estimate
				clamped.OutputTokens = maxOutput
				clamp = int(min(maxOutput, int64(math.MaxInt32)))
				need = aigateway.CostMicro(in.Model.Price, clamped, factor)
			}
		}
		if need > budget {
			switch {
			case budget == keyRemaining && keyRemaining < totalAvailable && keyRemaining <= spendRemaining:
				return nil, ErrAIGatewayKeySpendLimit
			case budget == spendRemaining && spendRemaining < totalAvailable:
				return nil, ErrAIGatewayDailyLimit
			default:
				return nil, &AIGatewayInsufficientError{Need: estimate, Available: totalAvailable}
			}
		}
	}

	holds, _ := aigateway.PlanHolds(available, need)
	for i := range holds {
		hold := &holds[i]
		switch hold.Source {
		case aigateway.SourceFreeDaily, aigateway.SourceVipDaily, aigateway.SourceVipMonthly:
			if err := adjustAIGatewayQuotaTx(ctx, tx, in.AppID, in.UserID, hold.Source, hold.Period, 0, hold.Micro); err != nil {
				return nil, err
			}
		case aigateway.SourceCredit:
			ledger.account.Credit -= hold.Micro
			ledger.account.CreditFrozen += hold.Micro
		case aigateway.SourceIntegral:
			hold.Units = aigateway.CeilUnits(hold.Micro, aigateway.MicroPerPoint)
			if err := ledger.moveIntegral(ctx, -hold.Units); err != nil {
				return nil, err
			}
		case aigateway.SourceWallet:
			hold.Units = aigateway.WalletHoldCents(hold.Micro, settings.WalletRate)
			if err := ledger.freezeWallet(ctx, hold.Units); err != nil {
				return nil, err
			}
		}
	}
	if snapshot.SpendLimit > 0 && need > 0 {
		if err := adjustAIGatewayQuotaTx(ctx, tx, in.AppID, in.UserID, aigateway.SpendDaily, snapshot.SpendPeriod, 0, need); err != nil {
			return nil, err
		}
	}
	if keyID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE ai_gateway_keys SET reserved = reserved + $2, last_used_at = $3,
last_used_ip = $4 WHERE id = $1`, keyID, need, now, nullableString(truncateColumn(in.ClientIP, 64))); err != nil {
			return nil, err
		}
	}
	if err := ledger.saveAccount(ctx); err != nil {
		return nil, err
	}

	priceJSON, err := json.Marshal(in.Model.Price)
	if err != nil {
		return nil, err
	}
	holdsJSON, _ := json.Marshal(holds)
	snapshotJSON, _ := json.Marshal(snapshot)
	ttl := aigateway.HoldTTL
	if in.Stream {
		ttl = aigateway.StreamHoldTTL
	}
	requestNo := generateTransactionNo("AIG")
	if _, err := tx.Exec(ctx, `INSERT INTO ai_gateway_requests
(request_no, appid, user_id, key_id, key_hint, auth_kind, endpoint, model, upstream_model, config_id, stream, status,
 price, factor, estimate, reserved, holds, billing, clamp_max_tokens, idempotency_key, client_ip, user_agent,
 created_at, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'reserved',$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
		requestNo, in.AppID, in.UserID, keyID, keyHint, in.AuthKind, in.Endpoint, in.Model.Model, in.Model.Upstream(),
		in.Model.ConfigID, in.Stream, priceJSON, factor.String(), estimate, need, holdsJSON, snapshotJSON, clamp,
		nullableString(idempotencyKey), nullableString(truncateColumn(in.ClientIP, 64)),
		nullableString(truncateColumn(in.UserAgent, 256)), now, now.Add(ttl)); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "idempotency") {
			// 并发的同键请求先插进去了：事务已中止，另开连接查它的编号，调用方据此重放那一次。
			conflict := &AIGatewayIdempotencyError{Status: aigateway.StatusReserved}
			_ = r.pool.QueryRow(context.WithoutCancel(ctx), `SELECT request_no, status FROM ai_gateway_requests
WHERE user_id = $1 AND idempotency_key = $2 AND status NOT IN ('failed', 'expired') LIMIT 1`, in.UserID, idempotencyKey).
				Scan(&conflict.RequestNo, &conflict.Status)
			return nil, conflict
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return &aigateway.ReserveResult{RequestNo: requestNo, Factor: factor, Reserved: need, ClampMaxTokens: clamp}, nil
}

// ── 结算 ──

// chargeBook 结算时按来源累计的扣费（保持扣费顺序）。
type chargeBook struct {
	order []string
	items map[string]*aigateway.Charge
}

func newChargeBook() *chargeBook {
	return &chargeBook{items: map[string]*aigateway.Charge{}}
}

func (b *chargeBook) add(source, period string, micro int64) {
	if micro <= 0 {
		return
	}
	key := source + "|" + period
	item, ok := b.items[key]
	if !ok {
		item = &aigateway.Charge{Source: source, Period: period}
		b.items[key] = item
		b.order = append(b.order, key)
	}
	item.Micro += micro
}

// sourceMicro 某个来源（不分周期）合计承担的微点。
func (b *chargeBook) sourceMicro(source string) int64 {
	var total int64
	for _, key := range b.order {
		if b.items[key].Source == source {
			total += b.items[key].Micro
		}
	}
	return total
}

func (b *chargeBook) charge(source string) *aigateway.Charge {
	for _, key := range b.order {
		if b.items[key].Source == source {
			return b.items[key]
		}
	}
	return nil
}

func (b *chargeBook) list() []aigateway.Charge {
	out := make([]aigateway.Charge, 0, len(b.order))
	for _, key := range b.order {
		out = append(out, *b.items[key])
	}
	return out
}

// describeAIGatewayUsage 流水里的一句话说明。
func describeAIGatewayUsage(model string, m aigateway.Meters) string {
	parts := make([]string, 0, 4)
	if m.InputTokens > 0 || m.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("输入 %d / 输出 %d tokens", m.InputTokens, m.OutputTokens))
	}
	if m.Images > 0 {
		parts = append(parts, fmt.Sprintf("%d 张图", m.Images))
	}
	if m.Characters > 0 {
		parts = append(parts, fmt.Sprintf("%d 字", m.Characters))
	}
	if m.AudioMillis > 0 {
		parts = append(parts, fmt.Sprintf("%s 秒音频", strconv.FormatFloat(float64(m.AudioMillis)/1000, 'f', -1, 64)))
	}
	if len(parts) == 0 {
		return model
	}
	return model + "：" + strings.Join(parts, "，")
}

// SettleAIGatewayRequest 按实际用量结算：放回暂扣、扣实际价格（各来源至多一条流水）。
//
// 重复结算返回第一次的结论。已被后台回收的暂扣（expired）照样结算：上游已经给出了结果，
// 这一笔从实时余额里扣，扣不上的记 shortfall。
func (r *Repository) SettleAIGatewayRequest(ctx context.Context, in aigateway.SettleInput) (*aigateway.Request, error) {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	status := in.Status
	if status != aigateway.StatusAborted {
		status = aigateway.StatusSucceeded
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

	req, err := lockAIGatewayRequestTx(ctx, tx, in.RequestNo, 0)
	if err != nil {
		return nil, err
	}
	if req.Status != aigateway.StatusReserved && req.Status != aigateway.StatusExpired {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		tx = nil
		return req, nil
	}
	late := req.Status == aigateway.StatusExpired
	ledger, err := lockAIGatewayUserTx(ctx, tx, req.AppID, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := lockAIGatewayKeyTx(ctx, tx, req.KeyID); err != nil {
		return nil, err
	}
	snapshot := req.Billing
	holds := req.Holds
	if late {
		holds = nil
	}
	if hasHoldSource(holds, aigateway.SourceWallet) || slices.Contains(snapshot.Sources, aigateway.SourceWallet) {
		if err := ledger.lockWallet(ctx); err != nil {
			return nil, err
		}
	}

	cost := aigateway.CostMicro(req.Price, in.Meters, req.Factor)
	consumed, overflow := aigateway.AllocateCost(holds, cost)
	book := newChargeBook()
	var integralHeld, walletHeld int64
	for i, hold := range holds {
		used := consumed[i]
		switch hold.Source {
		case aigateway.SourceFreeDaily, aigateway.SourceVipDaily, aigateway.SourceVipMonthly:
			if err := adjustAIGatewayQuotaTx(ctx, tx, req.AppID, req.UserID, hold.Source, hold.Period, used, -hold.Micro); err != nil {
				return nil, err
			}
		case aigateway.SourceCredit:
			ledger.account.CreditFrozen = max(ledger.account.CreditFrozen-hold.Micro, 0)
			ledger.account.Credit += hold.Micro - used
		case aigateway.SourceIntegral:
			integralHeld += hold.Units
		case aigateway.SourceWallet:
			walletHeld += hold.Units
		}
		book.add(hold.Source, hold.Period, used)
	}
	// 先把积分与余额的暂扣放回，下面一次性扣实际金额：流水上只有一条消费。
	if err := ledger.moveIntegral(ctx, integralHeld); err != nil {
		return nil, err
	}
	if err := ledger.freezeWallet(ctx, -walletHeld); err != nil {
		return nil, err
	}

	// 实际用量超出暂扣：按扣费顺序从实时余额补。积分与余额的可用部分要扣掉本笔暂扣放回的那部分，
	// 那部分已经预定给上面的结算了（推导见文件头的第 3 条不变式）。
	if overflow > 0 {
		loc := snapshot.Location()
		for _, source := range snapshot.Sources {
			if overflow == 0 {
				break
			}
			var available int64
			period := ""
			switch source {
			case aigateway.SourceFreeDaily, aigateway.SourceVipDaily, aigateway.SourceVipMonthly:
				meta, _ := aigateway.FindSource(source)
				limit := snapshot.Quotas[source]
				if limit <= 0 || (meta.VipOnly && !snapshot.Profile.IsVip) {
					continue
				}
				period = aigateway.PeriodKey(source, now, loc)
				used, reserved, err := lockAIGatewayQuotaTx(ctx, tx, req.AppID, req.UserID, source, period)
				if err != nil {
					return nil, err
				}
				available = max(limit-used-reserved, 0)
			case aigateway.SourceCredit:
				available = ledger.account.Credit
			case aigateway.SourceIntegral:
				available = saturatingMul(ledger.integral-integralHeld, aigateway.MicroPerPoint)
			case aigateway.SourceWallet:
				available = aigateway.WalletAvailableMicro(ledger.walletCents-walletHeld, snapshot.WalletRate)
			default:
				continue
			}
			take := min(available, overflow)
			if take <= 0 {
				continue
			}
			switch source {
			case aigateway.SourceFreeDaily, aigateway.SourceVipDaily, aigateway.SourceVipMonthly:
				if err := adjustAIGatewayQuotaTx(ctx, tx, req.AppID, req.UserID, source, period, take, 0); err != nil {
					return nil, err
				}
			case aigateway.SourceCredit:
				ledger.account.Credit -= take
			}
			book.add(source, period, take)
			overflow -= take
		}
	}
	shortfall := overflow
	description := describeAIGatewayUsage(req.Model, in.Meters)
	extra := map[string]any{"requestNo": req.RequestNo, "endpoint": req.Endpoint, "model": req.Model}

	// 积分：零头凑整后扣整数部分。暂扣与溢出的推导保证够扣；万一不够（例如别的路径绕过了暂扣直接改了余额），
	// 只扣得起的部分，其余记 shortfall —— 结算绝不因为这个失败，否则用户拿到了结果却永远不被扣费。
	integralTxnNo := ""
	if charge := book.charge(aigateway.SourceIntegral); charge != nil {
		micro := book.sourceMicro(aigateway.SourceIntegral)
		units, carry := aigateway.ChargeWithCarry(ledger.account.IntegralCarry, micro, aigateway.MicroPerPoint)
		if units > ledger.integral {
			shortfall += (units - ledger.integral) * aigateway.MicroPerPoint
			units = ledger.integral
		}
		ledger.account.IntegralCarry = carry
		charge.Units = units
		if units > 0 {
			result, err := applyIntegralChangeTxNo(ctx, tx, integralChange{
				UserID: req.UserID, AppID: req.AppID, Amount: -units, TxnType: "consume",
				Category: "ai_gateway", Title: "AI 调用", Description: description, SourceType: "ai_gateway",
				Extra: extra, ClientIP: req.ClientIP, UserAgent: req.UserAgent,
			})
			if err != nil {
				return nil, err
			}
			ledger.integral = result.After
			integralTxnNo = result.TransactionNo
			charge.TxnNo = integralTxnNo
		}
	}

	walletTxnNo := ""
	if charge := book.charge(aigateway.SourceWallet); charge != nil {
		micro := book.sourceMicro(aigateway.SourceWallet)
		microCents := aigateway.WalletMicroCents(micro, snapshot.WalletRate)
		units, carry := aigateway.ChargeWithCarry(ledger.account.WalletCarry, microCents, aigateway.MicroPerPoint)
		if units > ledger.walletCents {
			shortfall += aigateway.WalletAvailableMicro(units-ledger.walletCents, snapshot.WalletRate)
			units = ledger.walletCents
		}
		ledger.account.WalletCarry = carry
		charge.Units = units
		if units > 0 {
			result, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
				UserID: req.UserID, AppID: req.AppID, Amount: decimal.New(-units, -2),
				Type: walletdomain.TxnTypeConsume, Title: "AI 调用", Remark: description,
				IdempotencyKey: "aig:" + req.RequestNo, ClientIP: req.ClientIP, Metadata: extra,
			})
			if err != nil {
				return nil, err
			}
			ledger.walletCents -= units
			walletTxnNo = result.Transaction.TransactionNo
			charge.TxnNo = walletTxnNo
		}
	}

	if micro := book.sourceMicro(aigateway.SourceCredit); micro > 0 {
		if err := ledger.insertCredit(ctx, aigateway.CreditConsume, -micro, "ai_request", req.RequestNo, description, "", ""); err != nil {
			return nil, err
		}
	}

	shortfall = min(shortfall, cost)
	charged := cost - shortfall
	reservedRelease := req.ReservedMicro
	if late {
		reservedRelease = 0
	}
	if snapshot.SpendLimit > 0 && snapshot.SpendPeriod != "" {
		if err := adjustAIGatewayQuotaTx(ctx, tx, req.AppID, req.UserID, aigateway.SpendDaily, snapshot.SpendPeriod,
			charged, -reservedRelease); err != nil {
			return nil, err
		}
	}
	if req.KeyID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE ai_gateway_keys SET reserved = GREATEST(reserved - $2, 0), spent = spent + $3
WHERE id = $1`, req.KeyID, reservedRelease, charged); err != nil {
			return nil, err
		}
	}
	ledger.account.TotalSpent = saturatingAdd(ledger.account.TotalSpent, charged)
	ledger.account.TotalRequests++
	if err := ledger.saveAccount(ctx); err != nil {
		return nil, err
	}

	charges := book.list()
	chargesJSON, _ := json.Marshal(charges)
	upstreamModel := in.UpstreamModel
	if upstreamModel == "" {
		upstreamModel = req.UpstreamModel
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_gateway_requests SET status = $2, upstream_model = $3,
config_id = CASE WHEN $4::bigint > 0 THEN $4 ELSE config_id END, provider = CASE WHEN $5 <> '' THEN $5 ELSE provider END,
input_tokens = $6, output_tokens = $7, images = $8, characters = $9, audio_ms = $10, usage_estimated = $11,
cost = $12, charged = $13, shortfall = $14, charges = $15, integral_txn_no = $16, wallet_txn_no = $17,
latency_ms = $18, settled_at = $19
WHERE id = $1`, req.ID, status, upstreamModel, in.ConfigID, in.Provider, in.Meters.InputTokens, in.Meters.OutputTokens,
		in.Meters.Images, in.Meters.Characters, in.Meters.AudioMillis, in.UsageEstimated, cost, charged, shortfall,
		chargesJSON, nullableString(integralTxnNo), nullableString(walletTxnNo), in.LatencyMs, now); err != nil {
		return nil, err
	}
	settled, err := lockAIGatewayRequestTx(ctx, tx, req.RequestNo, 0)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return settled, nil
}

// ── 放回 ──

// ReleaseAIGatewayRequest 调用失败或暂扣超时：整笔放回，不写任何扣费流水。重复放回、已结算的记录原样返回。
func (r *Repository) ReleaseAIGatewayRequest(ctx context.Context, in aigateway.ReleaseInput) (*aigateway.Request, error) {
	status := in.Status
	if status != aigateway.StatusExpired {
		status = aigateway.StatusFailed
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
	req, err := lockAIGatewayRequestTx(ctx, tx, in.RequestNo, 0)
	if err != nil {
		return nil, err
	}
	if req.Status != aigateway.StatusReserved {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		tx = nil
		return req, nil
	}
	ledger, err := lockAIGatewayUserTx(ctx, tx, req.AppID, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := lockAIGatewayKeyTx(ctx, tx, req.KeyID); err != nil {
		return nil, err
	}
	if hasHoldSource(req.Holds, aigateway.SourceWallet) {
		if err := ledger.lockWallet(ctx); err != nil {
			return nil, err
		}
	}
	for _, hold := range req.Holds {
		switch hold.Source {
		case aigateway.SourceFreeDaily, aigateway.SourceVipDaily, aigateway.SourceVipMonthly:
			if err := adjustAIGatewayQuotaTx(ctx, tx, req.AppID, req.UserID, hold.Source, hold.Period, 0, -hold.Micro); err != nil {
				return nil, err
			}
		case aigateway.SourceCredit:
			ledger.account.CreditFrozen = max(ledger.account.CreditFrozen-hold.Micro, 0)
			ledger.account.Credit += hold.Micro
		case aigateway.SourceIntegral:
			if err := ledger.moveIntegral(ctx, hold.Units); err != nil {
				return nil, err
			}
		case aigateway.SourceWallet:
			if err := ledger.freezeWallet(ctx, -hold.Units); err != nil {
				return nil, err
			}
		}
	}
	snapshot := req.Billing
	if snapshot.SpendLimit > 0 && snapshot.SpendPeriod != "" {
		if err := adjustAIGatewayQuotaTx(ctx, tx, req.AppID, req.UserID, aigateway.SpendDaily, snapshot.SpendPeriod,
			0, -req.ReservedMicro); err != nil {
			return nil, err
		}
	}
	if req.KeyID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE ai_gateway_keys SET reserved = GREATEST(reserved - $2, 0) WHERE id = $1`,
			req.KeyID, req.ReservedMicro); err != nil {
			return nil, err
		}
	}
	if err := ledger.saveAccount(ctx); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_gateway_requests SET status = $2, error_code = $3, error_message = $4,
config_id = CASE WHEN $5::bigint > 0 THEN $5 ELSE config_id END, provider = CASE WHEN $6 <> '' THEN $6 ELSE provider END,
latency_ms = $7, settled_at = NOW()
WHERE id = $1`, req.ID, status, truncateColumn(in.ErrorCode, 64), truncateColumn(in.ErrorMessage, 1000),
		in.ConfigID, in.Provider, in.LatencyMs); err != nil {
		return nil, err
	}
	released, err := lockAIGatewayRequestTx(ctx, tx, req.RequestNo, 0)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return released, nil
}

// ListExpiredAIGatewayHolds 已过期仍在暂扣的调用编号（后台回收用，逐条走 ReleaseAIGatewayRequest）。
func (r *Repository) ListExpiredAIGatewayHolds(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT request_no FROM ai_gateway_requests
WHERE status = 'reserved' AND expires_at < $1 ORDER BY expires_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, limit)
	for rows.Next() {
		var no string
		if err := rows.Scan(&no); err != nil {
			return nil, err
		}
		out = append(out, no)
	}
	return out, rows.Err()
}

// ── 退款 ──

// RefundAIGatewayRequest 管理员把一次成功调用的扣费整笔退回各来源。
//
// 周期额度若已过了那个周期，退回到赠送额度（退回一个已经结束的周期等于没退）。
// 积分与余额按 RefundWithCarry 退：账户上「已扣单位 × 单位 + 零头 = 有效计价之和」在退款后仍成立。
func (r *Repository) RefundAIGatewayRequest(ctx context.Context, in aigateway.RefundInput) (*aigateway.Request, error) {
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
	req, err := lockAIGatewayRequestTx(ctx, tx, in.RequestNo, in.AppID)
	if err != nil {
		return nil, err
	}
	if req.Status != aigateway.StatusSucceeded && req.Status != aigateway.StatusAborted {
		return nil, ErrAIGatewayNotRefundable
	}
	ledger, err := lockAIGatewayUserTx(ctx, tx, req.AppID, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := lockAIGatewayKeyTx(ctx, tx, req.KeyID); err != nil {
		return nil, err
	}
	snapshot := req.Billing
	loc := snapshot.Location()
	reason := strings.TrimSpace(in.Reason)
	remark := "退款：" + req.Model
	if reason != "" {
		remark += "（" + reason + "）"
	}
	extra := map[string]any{"requestNo": req.RequestNo, "endpoint": req.Endpoint, "model": req.Model}

	var creditRefund int64
	for _, charge := range req.Charges {
		switch charge.Source {
		case aigateway.SourceFreeDaily, aigateway.SourceVipDaily, aigateway.SourceVipMonthly:
			if aigateway.PeriodKey(charge.Source, now, loc) == charge.Period {
				if err := adjustAIGatewayQuotaTx(ctx, tx, req.AppID, req.UserID, charge.Source, charge.Period, -charge.Micro, 0); err != nil {
					return nil, err
				}
			} else {
				creditRefund += charge.Micro
			}
		case aigateway.SourceCredit:
			creditRefund += charge.Micro
		case aigateway.SourceIntegral:
			units, carry := aigateway.RefundWithCarry(ledger.account.IntegralCarry, charge.Micro, aigateway.MicroPerPoint)
			ledger.account.IntegralCarry = carry
			if units > 0 {
				if _, err := applyIntegralChangeTxNo(ctx, tx, integralChange{
					UserID: req.UserID, AppID: req.AppID, Amount: units, TxnType: "earn",
					Category: "ai_gateway_refund", Title: "AI 调用退款", Description: remark, SourceType: "ai_gateway",
					Extra: extra,
				}); err != nil {
					return nil, err
				}
			}
		case aigateway.SourceWallet:
			microCents := aigateway.WalletMicroCents(charge.Micro, snapshot.WalletRate)
			units, carry := aigateway.RefundWithCarry(ledger.account.WalletCarry, microCents, aigateway.MicroPerPoint)
			ledger.account.WalletCarry = carry
			if units > 0 {
				if _, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
					UserID: req.UserID, AppID: req.AppID, Amount: decimal.New(units, -2),
					Type: walletdomain.TxnTypeAIRefund, Title: "AI 调用退款", Remark: remark,
					IdempotencyKey: "aig-refund:" + req.RequestNo, Operator: in.Operator, Metadata: extra,
					// 按原扣费的拆分退回：扣了多少赠送余额就先补回多少，余下进本金
					Restores: &walletdomain.RestoreRef{IdempotencyKey: "aig:" + req.RequestNo},
				}); err != nil {
					return nil, err
				}
			}
		}
	}
	if creditRefund > 0 {
		ledger.account.Credit = saturatingAdd(ledger.account.Credit, creditRefund)
		if err := ledger.insertCredit(ctx, aigateway.CreditRefund, creditRefund, "ai_request", req.RequestNo, remark, in.Operator, ""); err != nil {
			return nil, err
		}
	}
	if snapshot.SpendLimit > 0 && snapshot.SpendPeriod == aigateway.PeriodKey(aigateway.SpendDaily, now, loc) {
		if err := adjustAIGatewayQuotaTx(ctx, tx, req.AppID, req.UserID, aigateway.SpendDaily, snapshot.SpendPeriod,
			-req.ChargedMicro, 0); err != nil {
			return nil, err
		}
	}
	if req.KeyID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE ai_gateway_keys SET spent = GREATEST(spent - $2, 0) WHERE id = $1`,
			req.KeyID, req.ChargedMicro); err != nil {
			return nil, err
		}
	}
	ledger.account.TotalSpent = max(ledger.account.TotalSpent-req.ChargedMicro, 0)
	if err := ledger.saveAccount(ctx); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_gateway_requests SET status = 'refunded', refund_reason = $2, refunded_by = $3,
refunded_at = NOW() WHERE id = $1`, req.ID, nullableString(truncateColumn(reason, 200)), nullableString(in.Operator)); err != nil {
		return nil, err
	}
	refunded, err := lockAIGatewayRequestTx(ctx, tx, req.RequestNo, 0)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return refunded, nil
}

// ── 赠送额度 ──

// AdjustAIGatewayCredit 管理员增减赠送额度。RequestID 相同的请求只生效一次（重放返回当前余额）。
func (r *Repository) AdjustAIGatewayCredit(ctx context.Context, in aigateway.CreditAdjustInput) (*aigateway.CreditAdjustResult, error) {
	micro := aigateway.PointsToMicroFloor(in.Amount.Abs())
	if in.Amount.Sign() < 0 {
		micro = -micro
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
	if in.RequestID != "" {
		var userID, amount int64
		err := tx.QueryRow(ctx, `SELECT user_id, amount FROM ai_gateway_credit_ledger WHERE appid = $1 AND request_id = $2`,
			in.AppID, in.RequestID).Scan(&userID, &amount)
		if err == nil {
			if userID != in.UserID || amount != micro {
				return nil, ErrAIGatewayCreditRequestReused
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			tx = nil
			return &aigateway.CreditAdjustResult{Balance: aigateway.MicroToPoints(ledger.account.Credit), Replayed: true}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	next := ledger.account.Credit + micro
	if next < 0 {
		return nil, ErrAIGatewayCreditInsufficient
	}
	ledger.account.Credit = next
	if err := ledger.saveAccount(ctx); err != nil {
		return nil, err
	}
	if err := ledger.insertCredit(ctx, aigateway.CreditAdjust, micro, "admin", "", in.Reason, in.Operator, in.RequestID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return &aigateway.CreditAdjustResult{Balance: aigateway.MicroToPoints(next)}, nil
}

// grantAIGatewayCreditTx 在调用方的事务里发放赠送额度（激励广告）。调用方已锁住用户行。
func grantAIGatewayCreditTx(ctx context.Context, tx pgx.Tx, appID, userID, micro int64, refType, refID, remark string) (int64, error) {
	if micro <= 0 {
		return 0, errors.New("ai gateway: credit grant must be positive")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ai_gateway_accounts (user_id, appid) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING`,
		userID, appID); err != nil {
		return 0, err
	}
	var balance int64
	if err := tx.QueryRow(ctx, `UPDATE ai_gateway_accounts SET credit = credit + $2, updated_at = NOW()
WHERE user_id = $1 RETURNING credit`, userID, micro).Scan(&balance); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ai_gateway_credit_ledger (appid, user_id, kind, amount, balance_after, ref_type, ref_id, remark)
VALUES ($1, $2, 'grant', $3, $4, $5, $6, $7)`, appID, userID, micro, balance, refType, refID, truncateColumn(remark, 200)); err != nil {
		return 0, err
	}
	return balance, nil
}
