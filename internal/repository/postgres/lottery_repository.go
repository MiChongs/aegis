package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"

	"aegis/internal/domain/featuretime"
	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
)

// 抽奖：活动与奖品的维护、单事务抽奖、人工发放的处理与统计。规则见 docs/lottery.md。

var (
	ErrLotteryActivityNotFound = errors.New("lottery activity not found")
	ErrLotteryDrawNotFound     = errors.New("lottery draw not found")
	// ErrLotteryDrawState 只有待发放的记录能被标记发放
	ErrLotteryDrawState = errors.New("lottery draw is not pending")
	// ErrLotteryActivityHasDraws 有抽奖记录的活动不能删除，只能结束
	ErrLotteryActivityHasDraws = errors.New("lottery activity has draws")
	// ErrLotteryPoolBroken 奖池里找不到可发的奖品（保存时的校验被绕过了）
	ErrLotteryPoolBroken = errors.New("lottery prize pool has no drawable prize")
	// ErrLotteryRewardUnavailable 奖品引用的套餐或功能已失效
	ErrLotteryRewardUnavailable = errors.New("lottery prize reward unavailable")
)

// LotteryUnavailableError 这一次不能抽（原因随错误带出，与 Chances 同一套判据）。
type LotteryUnavailableError struct {
	Reason string
}

func (e *LotteryUnavailableError) Error() string { return "lottery unavailable: " + e.Reason }

// LotteryStockError 改库存时把库存改到了已抽中数量以下。
type LotteryStockError struct {
	Position int
	Used     int64
}

func (e *LotteryStockError) Error() string {
	return fmt.Sprintf("lottery prize at position %d already used %d", e.Position, e.Used)
}

func lotteryUnavailable(reason string) error { return &LotteryUnavailableError{Reason: reason} }

// lotteryActivityLockClass 活动咨询锁的类号（两段式键的第一段，第二段是活动 ID）：抽奖拿共享锁、改奖盘拿独占锁。
const lotteryActivityLockClass int32 = 0x4C4F5454 // "LOTT"

// lotteryLockTimeout 抽奖事务里等锁的上限。同一个人的抽奖在本实例内已经排队（LotteryService 的人级闸门），
// 这里等的只会是别的实例或别的功能（兑换、签到）占着同一个人的行，等不到就报繁忙，不占连接池。
const lotteryLockTimeout = "3s"

// ── 扫描 ──

const lotteryActivityColumns = `a.id, a.appid, a.name, a.description, a.rules, a.ui_mode, a.status, a.start_time, a.end_time,
a.free_daily, a.points_cost, a.points_daily, a.video_scene, a.pity_threshold, a.pity_prize_id, a.lifetime_policy,
a.lifetime_points_per_day, a.lifetime_points_lifetime, a.lifetime_draws, a.strategy, a.perks, a.sort_order,
COALESCE(a.created_by, ''), a.created_at, a.updated_at`

func scanLotteryActivity(row interface{ Scan(dest ...any) error }, extra ...any) (*lotterydomain.Activity, error) {
	var item lotterydomain.Activity
	var strategy, perks []byte
	dest := []any{
		&item.ID, &item.AppID, &item.Name, &item.Description, &item.Rules, &item.UIMode, &item.Status,
		&item.StartTime, &item.EndTime, &item.FreeDaily, &item.PointsCost, &item.PointsDaily, &item.VideoScene,
		&item.PityThreshold, &item.PityPrizeID, &item.LifetimePolicy, &item.LifetimePointsPerDay,
		&item.LifetimePointsLifetime, &item.LifetimeDraws, &strategy, &perks, &item.SortOrder, &item.CreatedBy, &item.CreatedAt,
		&item.UpdatedAt,
	}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLotteryActivityNotFound
		}
		return nil, err
	}
	// 坏数据不拦抽奖：解析失败按没有策略处理（保存时已校验）
	if len(strategy) > 0 {
		_ = json.Unmarshal(strategy, &item.Strategy)
		item.Strategy = lotterydomain.NormalizeStrategy(item.Strategy)
	}
	if len(perks) > 0 {
		_ = json.Unmarshal(perks, &item.Perks)
	}
	item.Perks = lotterydomain.NormalizePerks(item.Perks)
	return &item, nil
}

const lotteryPrizeColumns = `p.id, p.activity_id, p.name, p.kind, p.reward, COALESCE(p.image_url, ''), p.quantity, ` + lotteryPrizeUsedSQL + `,
p.weight, p.position, p.is_default, p.lifetime_reward, p.daily_limit, p.user_limit, p.user_daily_limit, p.points_value, p.created_at,
p.updated_at`

func scanLotteryPrize(row interface{ Scan(dest ...any) error }) (*lotterydomain.Prize, error) {
	var item lotterydomain.Prize
	var reward, swap []byte
	var quantity, used int32
	if err := row.Scan(&item.ID, &item.ActivityID, &item.Name, &item.Kind, &reward, &item.ImageURL, &quantity, &used,
		&item.Weight, &item.Position, &item.Fallback, &swap, &item.DailyLimit, &item.UserLimit, &item.UserDailyLimit,
		&item.Value, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if len(swap) > 0 {
		var alt lotterydomain.Reward
		if err := json.Unmarshal(swap, &alt); err == nil && alt.Type != "" {
			item.LifetimeReward = &alt
		}
	}
	item.Quantity, item.Used = int64(quantity), int64(used)
	if err := json.Unmarshal(reward, &item.Reward); err != nil || item.Reward.Type == "" {
		item.Reward = lotterydomain.Reward{Type: item.Kind}
	}
	return &item, nil
}

const lotteryDrawColumns = `d.id, d.draw_no, d.appid, d.activity_id, COALESCE(a.name, ''), d.user_id, COALESCE(u.account, ''),
COALESCE(up.nickname, ''), d.prize_id, d.prize_name, d.prize_kind, d.reward, d.position, d.source, d.points_cost, d.pity,
d.status, d.results, d.conversion, COALESCE(d.note, ''), COALESCE(d.handled_by, ''), d.handled_at, COALESCE(d.request_id, ''),
COALESCE(d.device_id, ''), COALESCE(d.client_ip, ''), d.trace, d.created_at, d.ticket_lot_id, d.ticket_origin, d.ticket_paid,
d.risk_rules, COALESCE(d.risk_action, '')`

const lotteryDrawJoins = `FROM lottery_draws d
LEFT JOIN lottery_activities a ON a.id = d.activity_id
LEFT JOIN users u ON u.id = d.user_id
LEFT JOIN user_profiles up ON up.user_id = d.user_id`

func scanLotteryDraw(row interface{ Scan(dest ...any) error }) (*lotterydomain.Draw, error) {
	var item lotterydomain.Draw
	var reward, results, conversion, trace []byte
	var ticketLot *int64
	var ticketOrigin string
	var ticketPaid bool
	if err := row.Scan(&item.ID, &item.DrawNo, &item.AppID, &item.ActivityID, &item.ActivityName, &item.UserID,
		&item.Account, &item.Nickname, &item.PrizeID, &item.PrizeName, &item.PrizeKind, &reward, &item.Position,
		&item.Source, &item.PointsCost, &item.Pity, &item.Status, &results, &conversion, &item.Note, &item.HandledBy,
		&item.HandledAt, &item.RequestID, &item.DeviceID, &item.ClientIP, &trace, &item.CreatedAt,
		&ticketLot, &ticketOrigin, &ticketPaid, &item.RiskRules, &item.RiskAction); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLotteryDrawNotFound
		}
		return nil, err
	}
	if item.Source == lotterydomain.SourceBonus {
		// 分批记账之前的记录没有批次：只知道用的是次数
		origin := ticketOrigin
		if origin == "" {
			origin = lotterydomain.TicketOriginLegacy
		}
		item.Ticket = &lotterydomain.DrawTicket{Origin: origin, OriginLabel: lotterydomain.TicketOriginLabel(origin), Paid: ticketPaid}
		if ticketLot != nil {
			item.Ticket.LotID = *ticketLot
		}
	}
	if err := json.Unmarshal(reward, &item.Reward); err != nil || item.Reward.Type == "" {
		item.Reward = lotterydomain.Reward{Type: item.PrizeKind}
	}
	_ = json.Unmarshal(results, &item.Results)
	if len(conversion) > 0 {
		var c lotterydomain.Conversion
		if err := json.Unmarshal(conversion, &c); err == nil && c.Reward.Type != "" {
			item.Conversion = &c
		}
	}
	if item.Results == nil {
		item.Results = []lotterydomain.RewardResult{}
	}
	if len(trace) > 0 {
		var plan lotterydomain.DrawPlan
		if err := json.Unmarshal(trace, &plan); err == nil && len(plan.Odds) > 0 {
			item.Trace = &plan
		}
	}
	return &item, nil
}

// ── 活动与奖品 ──

// GetLotteryActivity 取一个活动（必须属于这个应用）。
func (r *Repository) GetLotteryActivity(ctx context.Context, appID, activityID int64) (*lotterydomain.Activity, error) {
	return scanLotteryActivity(r.pool.QueryRow(ctx, `SELECT `+lotteryActivityColumns+`
FROM lottery_activities a WHERE a.id = $1 AND a.appid = $2`, activityID, appID))
}

// ListLotteryPrizes 一个活动的奖品，按位置排序。
func (r *Repository) ListLotteryPrizes(ctx context.Context, activityID int64) ([]lotterydomain.Prize, error) {
	return listLotteryPrizes(ctx, r.pool, activityID)
}

func listLotteryPrizes(ctx context.Context, q queryExecutor, activityID int64) ([]lotterydomain.Prize, error) {
	rows, err := q.Query(ctx, listLotteryPrizesSQL, activityID)
	if err != nil {
		return nil, err
	}
	return scanLotteryPrizeRows(rows)
}

// listLotteryPrizesSQL 活动的全部奖品（$1 活动 ID）。
const listLotteryPrizesSQL = `SELECT ` + lotteryPrizeColumns + `
FROM lottery_prizes p WHERE p.activity_id = $1 ORDER BY p.position, p.id`

// scanLotteryPrizeRows 读完并关闭 rows（单发与批量管道共用）。
func scanLotteryPrizeRows(rows pgx.Rows) ([]lotterydomain.Prize, error) {
	defer rows.Close()
	out := make([]lotterydomain.Prize, 0, 8)
	for rows.Next() {
		prize, err := scanLotteryPrize(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *prize)
	}
	return out, rows.Err()
}

// ListLotteryActivities 应用的活动（activeOnly 只取 active 状态，时间窗由调用方判）。
func (r *Repository) ListLotteryActivities(ctx context.Context, appID int64, activeOnly bool) ([]lotterydomain.Activity, error) {
	query := `SELECT ` + lotteryActivityColumns + ` FROM lottery_activities a WHERE a.appid = $1`
	if activeOnly {
		query += ` AND a.status = 'active'`
	}
	query += ` ORDER BY a.sort_order, a.id DESC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]lotterydomain.Activity, 0, 4)
	for rows.Next() {
		item, err := scanLotteryActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// lotteryActivitySummarySQL 活动的汇总（$2 是今天零点）：抽奖次数取全站计数（日计数 + 还没合并的增量）、参与人数取状态行，
// 不扫抽奖记录；待发放走 status = 'pending' 的部分索引。
const lotteryActivitySummarySQL = `SELECT
    COALESCE((SELECT SUM(x.draws) FROM lottery_activity_daily x WHERE x.activity_id = a.id), 0)
      + COALESCE((SELECT SUM(x.draws) FROM lottery_activity_deltas x WHERE x.activity_id = a.id), 0) AS total_draws,
    (SELECT COUNT(*) FROM lottery_user_states x WHERE x.activity_id = a.id) AS total_users,
    COALESCE((SELECT SUM(x.draws) FROM lottery_activity_daily x WHERE x.activity_id = a.id AND x.day_start = $2), 0)
      + COALESCE((SELECT SUM(x.draws) FROM lottery_activity_deltas x WHERE x.activity_id = a.id AND x.day_start = $2), 0) AS today_draws,
    (SELECT COUNT(*) FROM lottery_draws d WHERE d.appid = a.appid AND d.activity_id = a.id AND d.status = 'pending') AS pending`

// ListLotteryAdminActivities 管理端活动列表：活动、奖品与汇总。
func (r *Repository) ListLotteryAdminActivities(ctx context.Context, appID int64, dayStart time.Time) ([]lotterydomain.AdminActivity, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+lotteryActivityColumns+`,
  COALESCE(s.total_draws, 0), COALESCE(s.total_users, 0), COALESCE(s.today_draws, 0), COALESCE(s.pending, 0)
FROM lottery_activities a
LEFT JOIN LATERAL (`+lotteryActivitySummarySQL+`) s ON TRUE
WHERE a.appid = $1
ORDER BY a.sort_order, a.id DESC`, appID, dayStart)
	if err != nil {
		return nil, err
	}
	out := make([]lotterydomain.AdminActivity, 0, 4)
	for rows.Next() {
		var item lotterydomain.AdminActivity
		activity, err := scanLotteryActivity(rows, &item.TotalDraws, &item.TotalUsers, &item.TodayDraws, &item.Pending)
		if err != nil {
			rows.Close()
			return nil, err
		}
		item.Activity = *activity
		out = append(out, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range out {
		prizes, err := r.ListLotteryPrizes(ctx, out[index].ID)
		if err != nil {
			return nil, err
		}
		out[index].Prizes = prizes
	}
	return out, nil
}

// GetLotteryAdminActivity 管理端单个活动（含奖品与汇总）。
func (r *Repository) GetLotteryAdminActivity(ctx context.Context, appID, activityID int64, dayStart time.Time) (*lotterydomain.AdminActivity, error) {
	var item lotterydomain.AdminActivity
	activity, err := scanLotteryActivity(r.pool.QueryRow(ctx, `SELECT `+lotteryActivityColumns+`,
  COALESCE(s.total_draws, 0), COALESCE(s.total_users, 0), COALESCE(s.today_draws, 0), COALESCE(s.pending, 0)
FROM lottery_activities a
LEFT JOIN LATERAL (`+strings.ReplaceAll(lotteryActivitySummarySQL, "$2", "$3")+`) s ON TRUE
WHERE a.id = $1 AND a.appid = $2`, activityID, appID, dayStart),
		&item.TotalDraws, &item.TotalUsers, &item.TodayDraws, &item.Pending)
	if err != nil {
		return nil, err
	}
	item.Activity = *activity
	item.Prizes, err = r.ListLotteryPrizes(ctx, activityID)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// SaveLotteryActivity 新建（activityID 为 0）或整体更新一个活动及其全部奖品（单事务）。
//
// 奖品按 ID 对上：带 ID 的更新（保留已抽中计数），不带的新建，原有而这次没带的删除
// （抽奖记录里有快照，删除奖品不影响历史）。保底奖按位置指定，存的是落库后的奖品 ID。
func (r *Repository) SaveLotteryActivity(ctx context.Context, activityID int64, input lotterydomain.ActivityInput) (*lotterydomain.Activity, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	strategy, err := json.Marshal(input.Strategy)
	if err != nil {
		return nil, err
	}
	perks, err := json.Marshal(lotterydomain.NormalizePerks(input.Perks))
	if err != nil {
		return nil, err
	}
	if activityID == 0 {
		if err := tx.QueryRow(ctx, `INSERT INTO lottery_activities
(appid, name, description, rules, ui_mode, status, start_time, end_time, free_daily, points_cost, points_daily,
 video_scene, pity_threshold, sort_order, created_by, lifetime_policy, lifetime_points_per_day,
 lifetime_points_lifetime, lifetime_draws, strategy, perks)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21) RETURNING id`,
			input.AppID, input.Name, input.Description, input.Rules, input.UIMode, input.Status, input.StartTime,
			input.EndTime, input.FreeDaily, input.PointsCost, input.PointsDaily, input.VideoScene, input.PityThreshold,
			input.SortOrder, nullableString(input.Operator), input.LifetimePolicy, input.LifetimePointsPerDay,
			input.LifetimePointsLifetime, input.LifetimeDraws, strategy, perks).Scan(&activityID); err != nil {
			return nil, err
		}
	} else {
		// 独占活动的咨询锁：与进行中的抽奖（共享锁）互斥，奖盘不会在一次抽奖的中途被换掉
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2::int)`, lotteryActivityLockClass,
			int32(activityID)); err != nil {
			return nil, err
		}
		tag, err := tx.Exec(ctx, `UPDATE lottery_activities SET
  name = $3, description = $4, rules = $5, ui_mode = $6, status = $7, start_time = $8, end_time = $9,
  free_daily = $10, points_cost = $11, points_daily = $12, video_scene = $13, pity_threshold = $14,
  sort_order = $15, lifetime_policy = $16, lifetime_points_per_day = $17, lifetime_points_lifetime = $18,
  lifetime_draws = $19, strategy = $20, perks = $21, updated_at = NOW()
WHERE id = $1 AND appid = $2`, activityID, input.AppID, input.Name, input.Description, input.Rules, input.UIMode,
			input.Status, input.StartTime, input.EndTime, input.FreeDaily, input.PointsCost, input.PointsDaily,
			input.VideoScene, input.PityThreshold, input.SortOrder, input.LifetimePolicy, input.LifetimePointsPerDay,
			input.LifetimePointsLifetime, input.LifetimeDraws, strategy, perks)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, ErrLotteryActivityNotFound
		}
	}

	existing, err := listLotteryPrizes(ctx, tx, activityID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]lotterydomain.Prize, len(existing))
	for _, prize := range existing {
		byID[prize.ID] = prize
	}
	kept := make(map[int64]bool, len(input.Prizes))
	var pityPrizeID *int64
	for _, prize := range input.Prizes {
		reward, err := json.Marshal(prize.Reward)
		if err != nil {
			return nil, err
		}
		var swap []byte
		if prize.LifetimeReward != nil {
			if swap, err = json.Marshal(prize.LifetimeReward); err != nil {
				return nil, err
			}
		}
		var id int64
		if old, ok := byID[prize.ID]; ok && prize.ID > 0 {
			if prize.Quantity >= 0 && prize.Quantity < old.Used {
				return nil, &LotteryStockError{Position: prize.Position, Used: old.Used}
			}
			if _, err := tx.Exec(ctx, `UPDATE lottery_prizes SET
  name = $2, kind = $3, reward = $4, image_url = $5, quantity = $6, weight = $7, position = $8, is_default = $9,
  lifetime_reward = $10, daily_limit = $11, user_limit = $12, user_daily_limit = $13, points_value = $14, updated_at = NOW()
WHERE id = $1`, prize.ID, prize.Name, prize.Reward.Type, reward, nullableString(prize.ImageURL), prize.Quantity,
				prize.Weight, prize.Position, prize.Fallback, swap, prize.DailyLimit, prize.UserLimit,
				prize.UserDailyLimit, prize.Value); err != nil {
				return nil, err
			}
			id = prize.ID
			if old.Quantity != prize.Quantity || old.DailyLimit != prize.DailyLimit {
				if err := rebalanceLotteryStockTx(ctx, tx, id, int64(prize.Quantity), int64(prize.DailyLimit)); err != nil {
					return nil, err
				}
			}
		} else {
			// 带了不属于这个活动的 ID：按新建处理，不去碰别的活动的奖品
			if err := tx.QueryRow(ctx, `INSERT INTO lottery_prizes
(activity_id, name, kind, reward, image_url, quantity, weight, position, is_default, lifetime_reward,
 daily_limit, user_limit, user_daily_limit, points_value)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, activityID, prize.Name, prize.Reward.Type, reward,
				nullableString(prize.ImageURL), prize.Quantity, prize.Weight, prize.Position, prize.Fallback, swap,
				prize.DailyLimit, prize.UserLimit, prize.UserDailyLimit, prize.Value).Scan(&id); err != nil {
				return nil, err
			}
			if prize.Quantity >= 0 {
				if err := rebalanceLotteryStockTx(ctx, tx, id, int64(prize.Quantity), int64(prize.DailyLimit)); err != nil {
					return nil, err
				}
			}
		}
		kept[id] = true
		if input.PityThreshold > 0 && prize.Position == input.PityPosition {
			pityID := id
			pityPrizeID = &pityID
		}
	}
	for _, prize := range existing {
		if kept[prize.ID] {
			continue
		}
		if _, err := tx.Exec(ctx, `DELETE FROM lottery_prizes WHERE id = $1`, prize.ID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE lottery_activities SET pity_prize_id = $2 WHERE id = $1`, activityID, pityPrizeID); err != nil {
		return nil, err
	}

	activity, err := scanLotteryActivity(tx.QueryRow(ctx, `SELECT `+lotteryActivityColumns+`
FROM lottery_activities a WHERE a.id = $1`, activityID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return activity, nil
}

// SetLotteryActivityStatus 改状态（上线 / 暂停 / 结束）。
func (r *Repository) SetLotteryActivityStatus(ctx context.Context, appID, activityID int64, status string) (*lotterydomain.Activity, error) {
	return scanLotteryActivity(r.pool.QueryRow(ctx, `UPDATE lottery_activities a SET status = $3, updated_at = NOW()
WHERE a.id = $1 AND a.appid = $2 RETURNING `+lotteryActivityColumns, activityID, appID, status))
}

// DeleteLotteryActivity 删除没有抽奖记录的活动。有记录的只能结束：删掉它会连带删掉用户的中奖记录。
func (r *Repository) DeleteLotteryActivity(ctx context.Context, appID, activityID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM lottery_activities a WHERE a.id = $1 AND a.appid = $2
AND NOT EXISTS (SELECT 1 FROM lottery_draws d WHERE d.activity_id = a.id)`, activityID, appID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if _, err := r.GetLotteryActivity(ctx, appID, activityID); err != nil {
		return err
	}
	return ErrLotteryActivityHasDraws
}

// ── 用户的次数 ──

// LotteryUserFacts 一个人在一个活动里判定次数所需的事实（活动页用；抽奖时在事务里另取一份）。
func (r *Repository) LotteryUserFacts(ctx context.Context, appID, activityID, userID int64, dayStart time.Time) (lotterydomain.UserFacts, error) {
	counters, err := r.readLotteryCounters(ctx, activityID, userID, dayStart, time.Now())
	if err != nil {
		return lotterydomain.UserFacts{}, err
	}
	return lotteryUserFacts(ctx, r.pool, appID, userID, counters.today(dayStart), false)
}

// lotteryUserFacts 次数判定的事实：今天用了几次免费 / 积分（取自状态行的计数，counters 已对齐到今天）、
// 次数余额（lock 时锁汇总行）、积分余额、连续未中。
func lotteryUserFacts(ctx context.Context, q queryExecutor, appID, userID int64, counters lotteryCounters,
	lock bool) (lotterydomain.UserFacts, error) {
	facts := lotterydomain.UserFacts{
		FreeUsedToday: counters.FreeToday, PointsUsedToday: counters.PointsToday,
		MissStreak: counters.MissStreak, TotalDraws: counters.TotalDraws,
	}
	var err error
	if facts.Bonus, facts.BonusPurchased, err = lotteryTicketFactsQ(ctx, q, appID, userID, lock); err != nil {
		return facts, err
	}
	if err := q.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1 AND appid = $2`, userID, appID).
		Scan(&facts.Integral); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return facts, ErrUserNotFound
		}
		return facts, err
	}
	return facts, nil
}

// ── 已拥有 ──

// lotteryMemberCoverage 这个人已经拥有什么（vip.Holdings），以及应用的会员折算策略（折算积分的单价）。
func lotteryMemberCoverage(ctx context.Context, q queryExecutor, appID, userID int64) (lotterydomain.MemberCoverage, error) {
	holdings, err := vipHoldingsQ(ctx, q, appID, userID)
	if err != nil {
		return lotterydomain.MemberCoverage{}, err
	}
	policy, err := vipConversionPolicyQ(ctx, q, appID)
	if err != nil {
		return lotterydomain.MemberCoverage{}, err
	}
	return lotterydomain.MemberCoverage{Holdings: holdings, Policy: policy}, nil
}

// lotteryStoreDrawPriceQ 积分商城里一次抽奖次数的最低单价（商品积分 ÷ 其中抽奖次数的总数）；不卖时为 0。
//
// 含抽奖次数的组合商品也算（按全部积分摊给次数，偏低、偏保守）。不看上下架时段：今天不开放、明天开放，
// 环照样接得上。
func lotteryStoreDrawPriceQ(ctx context.Context, q queryExecutor, appID int64) (int64, error) {
	return scanLotteryStoreDrawPrice(q.QueryRow(ctx, lotteryStoreDrawPriceSQL, appID, pointstore.RewardLotteryDraws,
		pointstore.StatusActive))
}

// lotteryStoreDrawPriceSQL 见 lotteryStoreDrawPriceQ（$1 appid、$2 抽奖次数的奖励类型、$3 上架状态）。
const lotteryStoreDrawPriceSQL = `SELECT MIN(i.cost::float8 / d.draws)
FROM points_store_items i
CROSS JOIN LATERAL (
    SELECT SUM((r->>'amount')::float8) AS draws
    FROM jsonb_array_elements(i.rewards) r
    WHERE r->>'type' = $2 AND (r->>'amount')::float8 > 0
) d
WHERE i.appid = $1 AND i.status = $3 AND d.draws > 0`

// scanLotteryStoreDrawPrice 解析单价（单发与批量管道共用）。
func scanLotteryStoreDrawPrice(row pgx.Row) (int64, error) {
	var price *float64
	if err := row.Scan(&price); err != nil {
		return 0, err
	}
	if price == nil {
		return 0, nil
	}
	// 向下取整：单价 9.5 时按 9 封顶，宁可少给
	return max(int64(*price), 1), nil
}

// LotteryStoreDrawPrice 见 lotteryStoreDrawPriceQ（活动页的改发提示与控制台的套利检查用）。
func (r *Repository) LotteryStoreDrawPrice(ctx context.Context, appID int64) (int64, error) {
	return lotteryStoreDrawPriceQ(ctx, r.pool, appID)
}

// LotteryMemberCoverage 见 lotteryMemberCoverage（活动页用它标出「对你来说这一格会怎样」）。
func (r *Repository) LotteryMemberCoverage(ctx context.Context, appID, userID int64) (lotterydomain.MemberCoverage, error) {
	return lotteryMemberCoverage(ctx, r.pool, appID, userID)
}

// lotteryNeedsCatalog 奖盘里有没有要查套餐 / 功能目录才能判定或描述的奖品。
func lotteryNeedsCatalog(prizes []lotterydomain.Prize) bool {
	for _, prize := range prizes {
		switch prize.Kind {
		case lotterydomain.KindVipPlan, lotterydomain.KindFeature, lotterydomain.KindFeatureTime:
			return true
		}
	}
	return false
}

// ── 抽奖 ──

// ── 抽奖 ──（事务本身见 lottery_draw_tx.go）

// LotteryRiskDeniedError 风控规则判定拒绝这一次抽奖（事务回滚，次数与积分不扣）。
type LotteryRiskDeniedError struct {
	Plan lotterydomain.DrawPlan
}

func (e *LotteryRiskDeniedError) Error() string { return "lottery: draw denied by risk rules" }

// recordLotteryRiskEvent 记一条被拒绝的抽奖。写失败不影响给用户的答复（拒绝本身已经生效）。
func (r *Repository) recordLotteryRiskEvent(ctx context.Context, input lotterydomain.DrawInput, plan lotterydomain.DrawPlan) {
	report := plan.RiskReport
	if report == nil {
		return
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return
	}
	hits := report.Hits
	if hits == nil {
		hits = []string{}
	}
	_, _ = r.pool.Exec(context.WithoutCancel(ctx), `INSERT INTO lottery_risk_events
(appid, activity_id, user_id, action, rules, report, source, device_id, client_ip)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, input.AppID, input.ActivityID, input.UserID, report.Action, hits, raw,
		truncateColumn(input.Source, 16), nullableString(truncateColumn(input.DeviceID, 128)),
		nullableString(truncateColumn(input.ClientIP, 64)))
}

// recordLotteryConversionTx 抽中已拥有的奖品、按活动的处理方式改发之后，记一条会员折算记录。
//
// 改发成积分时积分就是这次折算的积分（流水号取发放结果）；改发成抽奖次数、经验等时积分为 0，
// 说明里写清改发了什么。这是已拥有的发放（owned），取消永久会员时不收回。
func recordLotteryConversionTx(ctx context.Context, tx pgx.Tx, appID, userID int64, rule lotterydomain.SwapRule,
	prize lotterydomain.Prize, plans map[int64]pointstore.PlanRef, conversion *lotterydomain.Conversion,
	results []lotterydomain.RewardResult) error {
	item := lotterydomain.OwnedItem(rule, prize, plans)
	record := &vipdomain.Conversion{
		AppID: appID, UserID: userID, Kind: vipdomain.ConversionOwned, Source: vipdomain.ChannelLottery,
		Reason: conversion.Reason, Message: conversion.Message,
	}
	if conversion.Reward.Type == lotterydomain.KindPoints {
		item.Points = conversion.Reward.Amount
		record.Points = conversion.Reward.Amount
		if len(results) > 0 {
			record.IntegralTransactionNo = results[0].TransactionNo
		}
	} else {
		item.Points = 0
	}
	record.Items = []vipdomain.ConversionItem{item}
	if err := insertVipConversionTx(ctx, tx, record); err != nil {
		return err
	}
	conversion.ConversionNo = record.ConversionNo
	return nil
}

// pickLotteryPrize 按出奖策略选奖品并占库存。返回选中的奖品与完整的出奖方案（写进记录的 trace）。
//
// 硬保底到点且保底奖这一次有资格时直接给它；否则按方案的最终权重抽（gonum 加权抽样，
// 随机源是 crypto/rand 播种的 ChaCha8）。选中后占每日限量与库存，被并发抢光时改发兜底奖，保底不算触发。
func pickLotteryPrize(ctx context.Context, tx pgx.Tx, activity lotterydomain.Activity, prizes []lotterydomain.Prize,
	state lotterydomain.DrawState, dayStart time.Time, userID int64) (lotterydomain.Prize, lotterydomain.DrawPlan, error) {
	plan := lotterydomain.PlanDraw(activity, activity.Strategy, prizes, state)
	if plan.RiskReport.Denied() {
		return lotterydomain.Prize{}, plan, &LotteryRiskDeniedError{Plan: plan}
	}
	fallback := func(reason string) (lotterydomain.Prize, lotterydomain.DrawPlan, error) {
		prize, ok := lotterydomain.FallbackPrize(prizes)
		if !ok {
			return prize, plan, ErrLotteryPoolBroken
		}
		plan.Pity, plan.Fallback, plan.FallbackReason, plan.Picked = false, true, reason, prize.ID
		return prize, plan, nil
	}

	index, pity := lotterydomain.PityChoice(activity, plan, prizes, state.MissStreak)
	if !pity {
		src, err := lotterydomain.NewDrawSource()
		if err != nil {
			return lotterydomain.Prize{}, plan, err
		}
		var ok bool
		if index, ok = lotterydomain.Sample(plan, src); !ok {
			return fallback(lotterydomain.FallbackEmpty)
		}
	}
	chosen := prizes[index]
	// 限量奖品在事务里从库存分片取一份（lottery_stock_repository.go）：分片按人错开，同一时刻抽中它的人分散在十几行上
	ok, err := reserveLotteryPrizeTx(ctx, tx, chosen, dayStart, userID)
	if err != nil {
		return chosen, plan, err
	}
	if !ok {
		return fallback(lotterydomain.FallbackContended)
	}
	plan.Pity, plan.Picked = pity, chosen.ID
	return chosen, plan, nil
}

// lotteryDrawNo 抽奖单号：LTD + UTC 秒 + 10 位加密随机数字。
func lotteryDrawNo(now time.Time) string {
	n, err := rand.Int(rand.Reader, big.NewInt(10_000_000_000))
	if err != nil {
		return generateTransactionNo("LTD")
	}
	return "LTD" + now.UTC().Format("20060102150405") + fmt.Sprintf("%010d", n.Int64())
}

// lotteryPrizeGrant 一次奖品发放的上下文。
type lotteryPrizeGrant struct {
	AppID  int64
	UserID int64
	DrawID int64
	DrawNo string
	Title  string
	Source string
	Meta   map[string]any
	// Plans / Features 抽奖事务里已经取过的目录（没取过时为 nil，按需再取）
	Plans    map[int64]pointstore.PlanRef
	Features map[string]pointstore.FeatureRef
}

// grantLotteryPrizeTx 在抽奖事务里发放奖品。
//
// 这里的 switch 与 lottery 的奖品目录**双向钉死**（TestLotteryCatalogHasGrantBranch）：
// 目录多一档 → 配得上却发不出来；这里多一档 → 控制台配不出来。
// 发不出去（套餐被删、功能停用）时整次抽奖回滚：次数与积分都没有花出去。
func (r *Repository) grantLotteryPrizeTx(ctx context.Context, tx pgx.Tx, grant lotteryPrizeGrant,
	prize lotterydomain.Prize) ([]lotterydomain.RewardResult, error) {
	reward := lotterydomain.NormalizeReward(prize.Reward)
	appID, userID := grant.AppID, grant.UserID
	drawID := grant.DrawID
	out := make([]lotterydomain.RewardResult, 0, 1)

	plans, features := grant.Plans, grant.Features
	if (reward.Type == lotterydomain.KindVipPlan || reward.Type == lotterydomain.KindFeature ||
		reward.Type == lotterydomain.KindFeatureTime) && plans == nil {
		var err error
		plans, features, err = pointsStoreCatalogRefs(ctx, tx, appID)
		if err != nil {
			return nil, err
		}
	}
	described := lotterydomain.DescribeReward(reward, plans, features)
	result := lotterydomain.RewardResult{Type: reward.Type, Label: described.Label, Detail: described.Detail}

	switch reward.Type {
	case lotterydomain.KindNone:
		return out, nil

	case lotterydomain.KindPoints:
		_, after, txnNo, err := applyIntegralChangeTx(ctx, tx, userID, appID, reward.Amount,
			"earn", lotterydomain.IntegralCategory, grant.Title, grant.Source, lotterydomain.SourceType, &drawID, grant.Meta)
		if err != nil {
			return nil, err
		}
		result.Detail += "（余额 " + strconv.FormatInt(after, 10) + "）"
		result.TransactionNo = txnNo

	case lotterydomain.KindExperience:
		expResult, err := r.applyExperienceChangeTx(ctx, tx, userID, appID, reward.Amount,
			lotterydomain.IntegralCategory, grant.Title, grant.Source, lotterydomain.SourceType, &drawID, grant.Meta)
		if err != nil {
			return nil, err
		}
		if expResult.LevelChanged {
			result.Detail += "（升至 Lv." + strconv.Itoa(expResult.NewLevel) + "）"
		}
		result.TransactionNo = expResult.TransactionNo

	case lotterydomain.KindVipDays:
		txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID: userID, AppID: appID, PlanName: grant.Title, DurationDays: int(reward.Amount),
			PayChannel: vipdomain.ChannelLottery, PayAmount: decimal.Zero, Metadata: grant.Meta,
		})
		if err != nil {
			return nil, err
		}
		result.TransactionNo = txn.TransactionNo

	case lotterydomain.KindVipPlan:
		plan, err := scanVipPlan(tx.QueryRow(ctx,
			`SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1 AND id = $2`, appID, reward.RefID))
		if err != nil {
			return nil, err
		}
		if plan == nil || plan.IsTrial() {
			return nil, ErrLotteryRewardUnavailable
		}
		planID := plan.ID
		txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID: userID, AppID: appID, PlanID: &planID, PlanName: plan.Name, Features: plan.Features,
			DurationDays: plan.DurationDays, Lifetime: plan.Lifetime,
			PayChannel: vipdomain.ChannelLottery, PayAmount: decimal.Zero, Metadata: grant.Meta,
		})
		if err != nil {
			return nil, err
		}
		result.Detail = plan.Name + "（" + plan.TermLabel() + "）"
		result.TransactionNo = txn.TransactionNo

	case lotterydomain.KindFeature:
		if feature, ok := features[reward.Tag]; !ok || !feature.IsActive {
			return nil, ErrLotteryRewardUnavailable
		}
		unlock, err := grantFeatureUnlockTx(ctx, tx, FeatureUnlockGrant{
			AppID: appID, UserID: userID, Tag: reward.Tag, Days: int(reward.Amount),
			Source: vipdomain.UnlockSourceLottery, SourceRef: grant.DrawNo,
		})
		if errors.Is(err, ErrFeatureUnlockOwned) {
			// 已永久拥有：没有可发的，记一笔说明而不是让这次抽奖失败
			result.Detail += "（已永久拥有）"
			break
		}
		if err != nil {
			return nil, err
		}
		result.ExpireAt = unlock.EndsAt
		result.TransactionNo = strconv.FormatInt(unlock.ID, 10)

	case lotterydomain.KindVipLifetime:
		// 与会员天数同口径：不挂套餐、不携带功能标识，只把这个人标记为永久会员
		txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID: userID, AppID: appID, PlanName: grant.Title, Lifetime: true,
			PayChannel: vipdomain.ChannelLottery, PayAmount: decimal.Zero, Metadata: grant.Meta,
		})
		if err != nil {
			return nil, err
		}
		result.TransactionNo = txn.TransactionNo

	case lotterydomain.KindFeatureTime:
		// 功能时长按秒记账，与激励广告发的是同一个账户；功能停用了也照发（标识本身就是账户的键）
		balance, err := grantFeatureTimeTx(ctx, tx, appID, userID, reward.Tag, reward.Amount*60,
			featuretime.RefLottery, grant.DrawNo, grant.Source)
		if err != nil {
			return nil, err
		}
		result.Detail += "（余额 " + strconv.FormatInt(featuretime.FormatMinutes(balance), 10) + " 分钟）"

	case lotterydomain.KindAdFree:
		expires, err := grantAdFreePassTx(ctx, tx, appID, userID, int(reward.Amount), time.Now())
		if err != nil {
			return nil, err
		}
		result.ExpireAt = &expires

	case lotterydomain.KindLotteryDraws:
		balance, err := grantLotteryDrawsTx(ctx, tx, lotterydomain.TicketGrant{
			AppID: appID, UserID: userID, Amount: reward.Amount, Origin: lotterydomain.TicketOriginLottery,
			Ref: grant.DrawNo, Title: strings.TrimSpace(grant.Title + " " + grant.Source),
		})
		if err != nil {
			return nil, err
		}
		// 写明是存进了次数余额：次数交给用户自己抽，不是「再来一次」那样当场又抽了
		result.Detail += "，已存入抽奖次数（现有 " + strconv.FormatInt(balance, 10) + " 次）"

	case lotterydomain.KindManual:
		result.Pending = true

	default:
		// 目录里有、这里没有 —— 由测试挡在合并之前
		return nil, fmt.Errorf("lottery: unsupported prize kind %s", reward.Type)
	}
	return append(out, result), nil
}

// ── 记录 ──

// GetLotteryDraw 取一条记录（必须属于这个应用）。
func (r *Repository) GetLotteryDraw(ctx context.Context, appID, drawID int64) (*lotterydomain.Draw, error) {
	return scanLotteryDraw(r.pool.QueryRow(ctx, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+`
WHERE d.id = $1 AND d.appid = $2`, drawID, appID))
}

// ListLotteryDraws 抽奖记录分页。
//
// 检索式见 lottery.ParseDrawSearch：每一段都落到有索引的列上（单号唯一索引、prize_name 与账号 / 昵称的
// 三元组索引、按应用的设备 / IP / 用户索引），账号与昵称先在用户表里解析成 user_id 集合再回到记录表，
// 不在三表 JOIN 之后做 OR 模糊匹配。总数与这一页在两个连接上并行查。
func (r *Repository) ListLotteryDraws(ctx context.Context, query lotterydomain.DrawQuery) (*lotterydomain.Page[lotterydomain.Draw], error) {
	where := []string{"d.appid = $1"}
	args := []any{query.AppID}
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	add := func(clause string, value any) {
		where = append(where, strings.ReplaceAll(clause, "?", arg(value)))
	}
	if query.ActivityID > 0 {
		add("d.activity_id = ?", query.ActivityID)
	}
	if query.UserID > 0 {
		add("d.user_id = ?", query.UserID)
	}
	if query.PrizeID > 0 {
		add("d.prize_id = ?", query.PrizeID)
	}
	if query.Status != "" {
		add("d.status = ?", query.Status)
	}
	if query.Source != "" {
		add("d.source = ?", query.Source)
	}
	if query.Winning {
		where = append(where, "d.prize_kind <> 'none'")
	}
	switch query.Risk {
	case "":
	case "any":
		where = append(where, "d.risk_action IS NOT NULL")
	case "clean":
		where = append(where, "d.risk_action IS NULL")
	default:
		add("d.risk_action IS NOT NULL AND d.risk_rules @> ARRAY[?]::text[]", query.Risk)
	}
	if query.RiskAction != "" {
		add("d.risk_action = ?", query.RiskAction)
	}
	switch query.Outcome {
	case "pity":
		where = append(where, "d.pity")
	case "fallback":
		where = append(where, "d.trace IS NOT NULL AND (d.trace->>'fallback') = 'true'")
	case "conversion":
		where = append(where, "d.conversion IS NOT NULL")
	}
	for _, term := range lotterydomain.ParseDrawSearch(query.Keyword) {
		where = append(where, lotteryDrawSearchClause(term, arg))
	}
	if query.Start != nil {
		add("d.created_at >= ?", *query.Start)
	}
	if query.End != nil {
		add("d.created_at < ?", *query.End)
	}
	whereSQL := strings.Join(where, " AND ")

	page, limit := query.Page, query.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	result := &lotterydomain.Page[lotterydomain.Draw]{Items: []lotterydomain.Draw{}, Page: page, Limit: limit}
	listArgs := append(append([]any{}, args...), limit, (page-1)*limit)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		// 计数不需要 JOIN 活动与用户资料：条件都在 d 上（账号已解析成 user_id 子查询）
		return r.pool.QueryRow(groupCtx, `SELECT COUNT(*) FROM lottery_draws d WHERE `+whereSQL, args...).Scan(&result.Total)
	})
	group.Go(func() error {
		rows, err := r.pool.Query(groupCtx, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+` WHERE `+whereSQL+`
ORDER BY d.created_at DESC, d.id DESC LIMIT $`+strconv.Itoa(len(listArgs)-1)+` OFFSET $`+strconv.Itoa(len(listArgs)), listArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanLotteryDraw(rows)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, *item)
		}
		return rows.Err()
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}
	result.TotalPages = int((result.Total + int64(limit) - 1) / int64(limit))
	return result, nil
}

// lotteryUserMatchSQL 按账号或昵称模糊匹配本应用的用户（$1 是 appid，两边各走自己的三元组索引）。
const lotteryUserMatchSQL = `SELECT u2.id FROM users u2 WHERE u2.appid = $1 AND u2.account ILIKE %[1]s
UNION SELECT up2.user_id FROM user_profiles up2 JOIN users u3 ON u3.id = up2.user_id AND u3.appid = $1
WHERE up2.nickname ILIKE %[1]s`

// lotteryDrawSearchClause 检索式的一段 → 条件。arg 登记参数并返回占位符。
func lotteryDrawSearchClause(term lotterydomain.SearchTerm, arg func(any) string) string {
	value := term.Value
	like := func() string { return arg("%" + escapeLikePattern(value) + "%") }
	users := func(pattern string) string { return fmt.Sprintf(lotteryUserMatchSQL, pattern) }
	switch term.Kind() {
	case lotterydomain.SearchNo:
		if len(value) == lotterydomain.DrawNoLength {
			return "d.draw_no = " + arg(strings.ToUpper(value))
		}
		return "d.draw_no LIKE " + arg(escapeLikePattern(strings.ToUpper(value))+"%")
	case lotterydomain.SearchIP:
		return "d.client_ip = " + arg(value)
	case lotterydomain.SearchDevice:
		return "d.device_id = " + arg(value)
	case lotterydomain.SearchPrize:
		return "d.prize_name ILIKE " + like()
	case lotterydomain.SearchUser:
		pattern := like()
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			return "(d.user_id = " + arg(id) + " OR d.user_id IN (" + users(pattern) + "))"
		}
		return "d.user_id IN (" + users(pattern) + ")"
	case lotterydomain.SearchID:
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "FALSE"
		}
		if term.Field == lotterydomain.SearchID {
			return "d.id = " + arg(id)
		}
		// 没指定字段的纯数字：记录 ID、用户 ID、数字账号（手机号之类），或奖品名里的数（「10」找「10 积分」）
		idArg := arg(id)
		return "(d.id = " + idArg + " OR d.user_id = " + idArg + " OR d.user_id IN (SELECT u2.id FROM users u2 WHERE u2.appid = $1 AND u2.account = " +
			arg(value) + ") OR d.prize_name ILIKE " + like() + ")"
	}
	pattern := like()
	return "(d.prize_name ILIKE " + pattern + " OR d.user_id IN (" + users(pattern) + "))"
}

// FulfillLotteryDraw 把一条待发放的记录标记为已发放。
func (r *Repository) FulfillLotteryDraw(ctx context.Context, input lotterydomain.HandleInput) (*lotterydomain.Draw, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM lottery_draws WHERE id = $1 AND appid = $2 FOR UPDATE`,
		input.DrawID, input.AppID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLotteryDrawNotFound
		}
		return nil, err
	}
	if status != lotterydomain.DrawPending {
		return nil, ErrLotteryDrawState
	}
	if _, err := tx.Exec(ctx, `UPDATE lottery_draws SET status = 'fulfilled', note = $2, handled_by = $3, handled_at = NOW(),
results = (SELECT COALESCE(jsonb_agg(item - 'pending'), '[]'::jsonb) FROM jsonb_array_elements(results) item)
WHERE id = $1`, input.DrawID, nullableString(strings.TrimSpace(input.Note)), nullableString(input.Operator)); err != nil {
		return nil, err
	}
	draw, err := scanLotteryDraw(tx.QueryRow(ctx, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+` WHERE d.id = $1`, input.DrawID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return draw, nil
}

// ── 统计 ──

// LotteryStats 一个活动的概览：窗口内按天趋势、按来源与按奖品的分布。缺的日期由服务层补零。
func (r *Repository) LotteryStats(ctx context.Context, appID, activityID int64, since, dayStart time.Time, timezone string) (*lotterydomain.Stats, error) {
	stats := &lotterydomain.Stats{ActivityID: activityID, Trend: []lotterydomain.StatsDay{},
		Sources: []lotterydomain.StatsSource{}, Prizes: []lotterydomain.StatsPrize{}}
	s := &stats.Summary
	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE created_at >= $3),
  COUNT(DISTINCT user_id) FILTER (WHERE created_at >= $3),
  COUNT(*) FILTER (WHERE created_at >= $3 AND prize_kind <> 'none'),
  COUNT(*), COUNT(DISTINCT user_id), COALESCE(SUM(points_cost), 0),
  COUNT(*) FILTER (WHERE status = 'pending'), COUNT(*) FILTER (WHERE pity),
  COUNT(*) FILTER (WHERE conversion IS NOT NULL),
  COALESCE(SUM((conversion->'reward'->>'amount')::BIGINT) FILTER (WHERE conversion->'reward'->>'type' = 'points'), 0)
FROM lottery_draws WHERE appid = $1 AND activity_id = $2`, appID, activityID, dayStart).Scan(
		&s.TodayDraws, &s.TodayUsers, &s.TodayWins, &s.TotalDraws, &s.TotalUsers, &s.PointsSpent, &s.Pending, &s.PityHits,
		&s.Converted, &s.ConvertedPoints); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $4, 'YYYY-MM-DD') AS day,
  COUNT(*), COUNT(*) FILTER (WHERE prize_kind <> 'none'), COUNT(DISTINCT user_id)
FROM lottery_draws WHERE appid = $1 AND activity_id = $2 AND created_at >= $3
GROUP BY day ORDER BY day`, appID, activityID, since, timezone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day lotterydomain.StatsDay
		if err := rows.Scan(&day.Date, &day.Draws, &day.Wins, &day.Users); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Trend = append(stats.Trend, day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `SELECT source, COUNT(*) FROM lottery_draws
WHERE appid = $1 AND activity_id = $2 AND created_at >= $3 GROUP BY source ORDER BY COUNT(*) DESC`, appID, activityID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item lotterydomain.StatsSource
		if err := rows.Scan(&item.Source, &item.Draws); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Sources = append(stats.Sources, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 现存奖品按位置列出（含窗口内一次没抽中的），已删除奖品的记录按快照名字归到末尾
	rows, err = r.pool.Query(ctx, `SELECT p.id, p.name, p.kind, p.quantity, `+lotteryPrizeUsedSQL+`,
  (SELECT COUNT(*) FROM lottery_draws d WHERE d.prize_id = p.id AND d.created_at >= $3),
  (SELECT COUNT(*) FROM lottery_draws d WHERE d.prize_id = p.id AND d.created_at >= $3 AND d.conversion IS NOT NULL)
FROM lottery_prizes p WHERE p.activity_id = $2 AND EXISTS (SELECT 1 FROM lottery_activities a WHERE a.id = $2 AND a.appid = $1)
ORDER BY p.position, p.id`, appID, activityID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item lotterydomain.StatsPrize
		var id int64
		var quantity, used int32
		if err := rows.Scan(&id, &item.Name, &item.Kind, &quantity, &used, &item.Draws, &item.Converted); err != nil {
			rows.Close()
			return nil, err
		}
		item.PrizeID, item.Quantity, item.Used = &id, int64(quantity), int64(used)
		stats.Prizes = append(stats.Prizes, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = r.pool.Query(ctx, `SELECT prize_name, prize_kind, COUNT(*) FROM lottery_draws
WHERE appid = $1 AND activity_id = $2 AND created_at >= $3 AND prize_id IS NULL
GROUP BY prize_name, prize_kind ORDER BY COUNT(*) DESC`, appID, activityID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item := lotterydomain.StatsPrize{Quantity: -1}
		if err := rows.Scan(&item.Name, &item.Kind, &item.Draws); err != nil {
			return nil, err
		}
		stats.Prizes = append(stats.Prizes, item)
	}
	return stats, rows.Err()
}
