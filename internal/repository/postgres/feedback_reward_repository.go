package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	ticketdomain "aegis/internal/domain/ticket"
)

// 有效反馈奖励积分（docs/feedback-rewards.md）。
//
// 发放与撤回各是一个事务，顺序固定：锁反馈行 → 查（锁）奖励记录 → 锁用户行 → 判上限 → 记积分流水 →
// 写 / 改奖励记录 → 时间线记一条。站内信经 NotificationService.Deliver 在提交后发（它要失效缓存、推实时事件，
// 不能在事务里做；全站的站内信只有这一个写入口，见 TestOnlyNotificationServiceCreatesUserNotifications）。
//
// 幂等：一张反馈同一时刻至多一条 granted（uq_feedback_rewards_active 兜底）。锁住反馈行之后再查，
// 同一张单子的并发请求排队，后到的看到前一个已提交的记录，原样返回（Replayed）或报已发过。

var (
	// ErrFeedbackRewardExists 这张反馈已有一笔数额不同的有效奖励
	ErrFeedbackRewardExists = errors.New("postgres: feedback already rewarded")
	// ErrFeedbackRewardNotFound 没有可撤回的奖励
	ErrFeedbackRewardNotFound = errors.New("postgres: no active feedback reward")
	// ErrFeedbackTicketGone 反馈在判定与加锁之间被删了，或已不是可发奖励的状态
	ErrFeedbackTicketGone = errors.New("postgres: feedback ticket not rewardable")
)

const feedbackRewardColumns = `id, ticket_id, appid, user_id, ticket_no, category_key, points, reason, status,
COALESCE(request_id, ''), grant_txn_no, granted_by_admin_id, granted_by_name, granted_at, notification_id,
revoked_at, revoked_by_admin_id, revoked_by_name, revoke_reason, COALESCE(revoke_request_id, ''),
COALESCE(revoke_txn_no, ''), clawed_back, shortfall`

func scanFeedbackReward(row pgx.Row) (*ticketdomain.Reward, error) {
	var item ticketdomain.Reward
	if err := row.Scan(&item.ID, &item.TicketID, &item.AppID, &item.UserID, &item.TicketNo, &item.CategoryKey,
		&item.Points, &item.Reason, &item.Status, &item.RequestID, &item.GrantTxnNo, &item.GrantedByAdminID,
		&item.GrantedByName, &item.GrantedAt, &item.NotificationID, &item.RevokedAt, &item.RevokedByAdminID,
		&item.RevokedByName, &item.RevokeReason, &item.RevokeRequestID, &item.RevokeTxnNo, &item.ClawedBack,
		&item.Shortfall); err != nil {
		return nil, err
	}
	return &item, nil
}

// ─────────────── 策略 ───────────────

// GetFeedbackRewardPolicy 应用的反馈奖励策略；没有配置时返回推荐默认值（未开启，Configured=false）。
func (r *Repository) GetFeedbackRewardPolicy(ctx context.Context, appID int64) (*ticketdomain.RewardPolicy, error) {
	return getFeedbackRewardPolicy(ctx, r.pool, appID)
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func getFeedbackRewardPolicy(ctx context.Context, q rowQuerier, appID int64) (*ticketdomain.RewardPolicy, error) {
	var (
		policy     ticketdomain.RewardPolicy
		categories []byte
		updatedAt  time.Time
	)
	err := q.QueryRow(ctx, `SELECT appid, enabled, default_points, category_points, max_points, user_daily_limit,
user_monthly_limit, updated_by_name, updated_at FROM feedback_reward_policies WHERE appid = $1`, appID).
		Scan(&policy.AppID, &policy.Enabled, &policy.DefaultPoints, &categories, &policy.MaxPoints,
			&policy.UserDailyLimit, &policy.UserMonthlyLimit, &policy.UpdatedByName, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		fallback := ticketdomain.DefaultRewardPolicy(appID)
		return &fallback, nil
	}
	if err != nil {
		return nil, err
	}
	policy.CategoryPoints = map[string]int64{}
	if len(categories) > 0 {
		_ = json.Unmarshal(categories, &policy.CategoryPoints)
	}
	policy.Configured = true
	policy.UpdatedAt = &updatedAt
	return &policy, nil
}

// SaveFeedbackRewardPolicy 写入（覆盖）应用的反馈奖励策略。调用方先经 NormalizeRewardPolicy 校验。
func (r *Repository) SaveFeedbackRewardPolicy(ctx context.Context, policy ticketdomain.RewardPolicy, adminID *int64, adminName string) (*ticketdomain.RewardPolicy, error) {
	categories, err := json.Marshal(policy.CategoryPoints)
	if err != nil {
		return nil, err
	}
	if policy.CategoryPoints == nil {
		categories = []byte("{}")
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO feedback_reward_policies (appid, enabled, default_points, category_points,
max_points, user_daily_limit, user_monthly_limit, updated_by_admin_id, updated_by_name)
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9)
ON CONFLICT (appid) DO UPDATE SET enabled = EXCLUDED.enabled, default_points = EXCLUDED.default_points,
    category_points = EXCLUDED.category_points, max_points = EXCLUDED.max_points,
    user_daily_limit = EXCLUDED.user_daily_limit, user_monthly_limit = EXCLUDED.user_monthly_limit,
    updated_by_admin_id = EXCLUDED.updated_by_admin_id, updated_by_name = EXCLUDED.updated_by_name, updated_at = NOW()`,
		policy.AppID, policy.Enabled, policy.DefaultPoints, categories, policy.MaxPoints,
		policy.UserDailyLimit, policy.UserMonthlyLimit, adminID, adminName); err != nil {
		return nil, err
	}
	return r.GetFeedbackRewardPolicy(ctx, policy.AppID)
}

// ─────────────── 查询 ───────────────

// ListFeedbackRewards 一张反馈的全部奖励记录（新的在前）。
func (r *Repository) ListFeedbackRewards(ctx context.Context, ticketID int64) ([]ticketdomain.Reward, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+feedbackRewardColumns+` FROM feedback_rewards WHERE ticket_id = $1 ORDER BY id DESC`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.Reward, 0, 2)
	for rows.Next() {
		item, err := scanFeedbackReward(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ActiveFeedbackRewards 这些反馈当前有效（已发放、未撤回）的奖励，用户侧列表与详情用。
func (r *Repository) ActiveFeedbackRewards(ctx context.Context, ticketIDs []int64) ([]ticketdomain.Reward, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+feedbackRewardColumns+` FROM feedback_rewards
WHERE ticket_id = ANY($1) AND status = 'granted'`, ticketIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.Reward, 0, len(ticketIDs))
	for rows.Next() {
		item, err := scanFeedbackReward(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// FeedbackRewardUsage 某用户在本应用自 dayStart / monthStart 起已领的反馈奖励积分（已撤回的不计）。
func (r *Repository) FeedbackRewardUsage(ctx context.Context, appID, userID int64, dayStart, monthStart time.Time) (ticketdomain.RewardUsage, error) {
	return feedbackRewardUsage(ctx, r.pool, appID, userID, dayStart, monthStart)
}

func feedbackRewardUsage(ctx context.Context, q rowQuerier, appID, userID int64, dayStart, monthStart time.Time) (ticketdomain.RewardUsage, error) {
	var usage ticketdomain.RewardUsage
	err := q.QueryRow(ctx, `SELECT
    COALESCE(SUM(points) FILTER (WHERE granted_at >= $3), 0)::bigint,
    COALESCE(SUM(points) FILTER (WHERE granted_at >= $4), 0)::bigint
FROM feedback_rewards
WHERE appid = $1 AND user_id = $2 AND status = 'granted' AND granted_at >= LEAST($3, $4)`,
		appID, userID, dayStart, monthStart).Scan(&usage.Daily, &usage.Monthly)
	return usage, err
}

// SetFeedbackRewardNotification 记下发给用户的那条站内信（提交后才知道 ID）。
func (r *Repository) SetFeedbackRewardNotification(ctx context.Context, rewardID, notificationID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE feedback_rewards SET notification_id = $2, updated_at = NOW() WHERE id = $1`, rewardID, notificationID)
	return err
}

// ─────────────── 发放 ───────────────

// GrantFeedbackRewardInput 一次发放。上限判定在事务里、锁住用户行之后做（同一用户的并发发放排队），
// 用 Policy 的快照与 DayStart / MonthStart 切分的用量。
type GrantFeedbackRewardInput struct {
	TicketID   int64
	Points     int64
	Reason     string
	RequestID  string
	AdminID    *int64
	AdminName  string
	Policy     ticketdomain.RewardPolicy
	DayStart   time.Time
	MonthStart time.Time
	ClientIP   string
	UserAgent  string
}

// GrantFeedbackRewardResult 发放结果。Replayed 表示这张单子已有同样的一笔，原样返回、没有再发。
type GrantFeedbackRewardResult struct {
	Reward        *ticketdomain.Reward
	BalanceBefore int64
	BalanceAfter  int64
	Replayed      bool
}

// GrantFeedbackReward 发放一笔反馈奖励（单事务）。
//
// 已有有效奖励时：请求编号相同或数额相同 → 原样返回（Replayed）；否则 ErrFeedbackRewardExists。
// 上限不够返回 *ticketdomain.RewardLimitError，用户不存在返回 ErrUserNotFound，均整笔回滚。
func (r *Repository) GrantFeedbackReward(ctx context.Context, in GrantFeedbackRewardInput) (*GrantFeedbackRewardResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	item, err := lockFeedbackTicketTx(ctx, tx, in.TicketID)
	if err != nil {
		return nil, err
	}
	if reason := ticketdomain.RewardIneligibleReason(item); reason != "" {
		return nil, ErrFeedbackTicketGone
	}
	active, err := scanFeedbackReward(tx.QueryRow(ctx, `SELECT `+feedbackRewardColumns+`
FROM feedback_rewards WHERE ticket_id = $1 AND status = 'granted' FOR UPDATE`, in.TicketID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if active != nil {
		if (in.RequestID != "" && active.RequestID == in.RequestID) || active.Points == in.Points {
			return &GrantFeedbackRewardResult{Reward: active, Replayed: true}, nil
		}
		return nil, ErrFeedbackRewardExists
	}
	if in.RequestID != "" {
		// 同一个请求编号已经发过（之后又被撤回）：这是旧请求的重放，不能再发一次
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM feedback_rewards WHERE ticket_id = $1 AND request_id = $2)`,
			in.TicketID, in.RequestID).Scan(&used); err != nil {
			return nil, err
		}
		if used {
			return nil, ErrRequestIDConflict
		}
	}

	userID := *item.RequesterUserID
	if err := lockUserRow(ctx, tx, userID, item.AppID); err != nil {
		return nil, err
	}
	usage, err := feedbackRewardUsage(ctx, tx, item.AppID, userID, in.DayStart, in.MonthStart)
	if err != nil {
		return nil, err
	}
	if err := in.Policy.CheckGrant(in.Points, usage); err != nil {
		return nil, err
	}

	description := fmt.Sprintf("反馈 %s 被采纳", item.TicketNo)
	if in.Reason != "" {
		description += "：" + in.Reason
	}
	extra := map[string]any{
		"ticketId": item.ID, "ticketNo": item.TicketNo, "reason": in.Reason,
		"clientIp": in.ClientIP, "userAgent": in.UserAgent,
	}
	if in.AdminID != nil {
		extra["adminId"] = *in.AdminID
	}
	extra["adminAccount"] = in.AdminName
	ticketID := item.ID
	change, err := applyIntegralChangeTxNo(ctx, tx, integralChange{
		UserID: userID, AppID: item.AppID, Amount: in.Points, TxnType: "earn", Category: "feedback_reward",
		Title: "反馈奖励", Description: description, SourceType: ticketdomain.RewardSourceType, SourceID: &ticketID,
		Extra: extra, TransactionNo: feedbackRewardTxnNo("FBR", item.ID, in.RequestID),
		ClientIP: in.ClientIP, UserAgent: in.UserAgent,
	})
	if err != nil {
		return nil, err
	}

	reward, err := scanFeedbackReward(tx.QueryRow(ctx, `INSERT INTO feedback_rewards (ticket_id, appid, user_id, ticket_no,
category_key, points, reason, status, request_id, grant_txn_no, granted_by_admin_id, granted_by_name)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'granted', $8, $9, $10, $11)
RETURNING `+feedbackRewardColumns,
		item.ID, item.AppID, userID, item.TicketNo, item.CategoryKey, in.Points, in.Reason,
		nullableString(in.RequestID), change.TransactionNo, in.AdminID, in.AdminName))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrFeedbackRewardExists
		}
		return nil, err
	}

	summary := fmt.Sprintf("%s 发放了反馈奖励 %d 积分", in.AdminName, in.Points)
	if in.Reason != "" {
		summary += "：" + in.Reason
	}
	if err := insertTicketEventTx(ctx, tx, ticketdomain.Event{
		TicketID: item.ID, Event: ticketdomain.EventRewardGranted, ActorType: "admin", ActorID: in.AdminID,
		ActorName: in.AdminName, ToValue: fmt.Sprint(in.Points), Summary: summary,
		Metadata: map[string]any{
			"rewardId": reward.ID, "points": in.Points, "reason": in.Reason, "transactionNo": change.TransactionNo,
			"balanceBefore": change.Before, "balanceAfter": change.After,
		},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &GrantFeedbackRewardResult{Reward: reward, BalanceBefore: change.Before, BalanceAfter: change.After}, nil
}

// ─────────────── 撤回 ───────────────

// RevokeFeedbackRewardInput 一次撤回。
type RevokeFeedbackRewardInput struct {
	TicketID     int64
	Reason       string
	AllowPartial bool
	RequestID    string
	AdminID      *int64
	AdminName    string
	ClientIP     string
	UserAgent    string
}

// RevokeFeedbackRewardResult 撤回结果。Replayed 表示同一请求编号已经撤回过，原样返回。
type RevokeFeedbackRewardResult struct {
	Reward        *ticketdomain.Reward
	BalanceBefore int64
	BalanceAfter  int64
	Replayed      bool
}

// RevokeFeedbackReward 撤回当前有效的奖励并扣回积分（单事务）。
//
// AllowPartial 为假时余额不足返回 *BalanceError（整笔回滚，与积分冲正一致）；为真时扣到 0 为止，差额记 shortfall。
// 用户已注销（user_id 为空）时只把记录标为撤回，没有可扣的积分，全额记 shortfall。
func (r *Repository) RevokeFeedbackReward(ctx context.Context, in RevokeFeedbackRewardInput) (*RevokeFeedbackRewardResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := lockFeedbackTicketTx(ctx, tx, in.TicketID); err != nil {
		return nil, err
	}
	active, err := scanFeedbackReward(tx.QueryRow(ctx, `SELECT `+feedbackRewardColumns+`
FROM feedback_rewards WHERE ticket_id = $1 AND status = 'granted' FOR UPDATE`, in.TicketID))
	if errors.Is(err, pgx.ErrNoRows) {
		if in.RequestID != "" {
			last, lerr := scanFeedbackReward(tx.QueryRow(ctx, `SELECT `+feedbackRewardColumns+`
FROM feedback_rewards WHERE ticket_id = $1 AND status = 'revoked' AND revoke_request_id = $2 ORDER BY id DESC LIMIT 1`,
				in.TicketID, in.RequestID))
			if lerr == nil {
				return &RevokeFeedbackRewardResult{Reward: last, Replayed: true}, nil
			}
			if !errors.Is(lerr, pgx.ErrNoRows) {
				return nil, lerr
			}
		}
		return nil, ErrFeedbackRewardNotFound
	}
	if err != nil {
		return nil, err
	}

	var (
		clawed, before, after int64
		txnNo                 string
	)
	shortfall := active.Points
	if active.UserID != nil {
		var balance int64
		err := tx.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, *active.UserID, active.AppID).Scan(&balance)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// 用户已不属于该应用：没有可扣的积分
		case err != nil:
			return nil, err
		default:
			before, after = balance, balance
			clawed = active.Points
			if in.AllowPartial {
				clawed = min(active.Points, max(balance, 0))
			}
			shortfall = active.Points - clawed
			if clawed > 0 {
				description := fmt.Sprintf("撤回反馈 %s 的奖励", active.TicketNo)
				if in.Reason != "" {
					description += "：" + in.Reason
				}
				extra := map[string]any{
					"ticketId": active.TicketID, "ticketNo": active.TicketNo, "rewardId": active.ID,
					"reversalOf": active.GrantTxnNo, "reason": in.Reason, "shortfall": shortfall,
					"adminAccount": in.AdminName, "clientIp": in.ClientIP, "userAgent": in.UserAgent,
				}
				if in.AdminID != nil {
					extra["adminId"] = *in.AdminID
				}
				ticketID := active.TicketID
				// 余额不足时 applyIntegralChangeTxNo 返回 *BalanceError，整笔回滚
				change, err := applyIntegralChangeTxNo(ctx, tx, integralChange{
					UserID: *active.UserID, AppID: active.AppID, Amount: -clawed, TxnType: "consume",
					Category: "feedback_reward_revoke", Title: "反馈奖励撤回", Description: description,
					SourceType: ticketdomain.RewardSourceType, SourceID: &ticketID, Extra: extra,
					TransactionNo: feedbackRewardTxnNo("FBV", active.ID, in.RequestID),
					ClientIP:      in.ClientIP, UserAgent: in.UserAgent,
				})
				if err != nil {
					return nil, err
				}
				before, after, txnNo = change.Before, change.After, change.TransactionNo
			}
		}
	}

	revoked, err := scanFeedbackReward(tx.QueryRow(ctx, `UPDATE feedback_rewards SET status = 'revoked', revoked_at = NOW(),
revoked_by_admin_id = $2, revoked_by_name = $3, revoke_reason = $4, revoke_request_id = $5, revoke_txn_no = $6,
clawed_back = $7, shortfall = $8, updated_at = NOW()
WHERE id = $1 RETURNING `+feedbackRewardColumns,
		active.ID, in.AdminID, in.AdminName, in.Reason, nullableString(in.RequestID), nullableString(txnNo), clawed, shortfall))
	if err != nil {
		return nil, err
	}

	summary := fmt.Sprintf("%s 撤回了反馈奖励，扣回 %d 积分", in.AdminName, clawed)
	if shortfall > 0 {
		summary += fmt.Sprintf("（余额不足，差 %d 未扣）", shortfall)
	}
	if in.Reason != "" {
		summary += "：" + in.Reason
	}
	if err := insertTicketEventTx(ctx, tx, ticketdomain.Event{
		TicketID: active.TicketID, Event: ticketdomain.EventRewardRevoked, ActorType: "admin", ActorID: in.AdminID,
		ActorName: in.AdminName, FromValue: fmt.Sprint(active.Points), Summary: summary,
		Metadata: map[string]any{
			"rewardId": active.ID, "points": active.Points, "clawedBack": clawed, "shortfall": shortfall,
			"reason": in.Reason, "transactionNo": txnNo, "reversalOf": active.GrantTxnNo,
		},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &RevokeFeedbackRewardResult{Reward: revoked, BalanceBefore: before, BalanceAfter: after}, nil
}

// lockFeedbackTicketTx 锁住反馈行并读出发奖励要用的字段。同一张单子的发放 / 撤回在这把锁上排队。
func lockFeedbackTicketTx(ctx context.Context, tx pgx.Tx, ticketID int64) (*ticketdomain.Ticket, error) {
	var item ticketdomain.Ticket
	err := tx.QueryRow(ctx, `SELECT t.id, t.ticket_no, t.kind, t.appid, t.requester_type, t.requester_user_id, t.status,
COALESCE((SELECT c.key FROM ticket_categories c WHERE c.id = t.category_id), '')
FROM tickets t WHERE t.id = $1 FOR UPDATE OF t`, ticketID).
		Scan(&item.ID, &item.TicketNo, &item.Kind, &item.AppID, &item.RequesterType, &item.RequesterUserID, &item.Status, &item.CategoryKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFeedbackTicketGone
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// insertTicketEventTx 事务内写一条时间线（与 AddTicketEvent 同一张表、同样的列）。
func insertTicketEventTx(ctx context.Context, tx pgx.Tx, item ticketdomain.Event) error {
	metadataJSON, _ := json.Marshal(orEmptyMap(item.Metadata))
	_, err := tx.Exec(ctx,
		`INSERT INTO ticket_events (ticket_id, event, actor_type, actor_id, actor_name, from_value, to_value, summary, metadata)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		item.TicketID, item.Event, item.ActorType, item.ActorID, item.ActorName,
		item.FromValue, item.ToValue, item.Summary, metadataJSON)
	return err
}

// feedbackRewardTxnNo 奖励流水号：带请求编号时由（对象, 编号）派生，否则随机生成。前缀 3 位 + 32 位十六进制，不超过 VARCHAR(40)。
func feedbackRewardTxnNo(prefix string, objectID int64, requestID string) string {
	if requestID == "" {
		return generateTransactionNo(prefix)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", prefix, objectID, requestID)))
	return prefix + hex.EncodeToString(sum[:16])
}
