package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	lotterydomain "aegis/internal/domain/lottery"

	"github.com/jackc/pgx/v5"
)

// ── 行为画像 ──

// lotteryBehaviorSQL 一个人在应用里的全部行为，一条语句取齐（都是按人的索引查询）。
// $1 user_id、$2 appid、$3 此刻、$4 应用时区的今天（DATE）。
//
// 最近一次在线取 user_online_daily.updated_at（最后一次累计的时刻），不能取 day::timestamptz：
// day 是 UTC 日期，转成时刻是那一天的零点、还随会话时区变，「最近活跃」会差出一天多。
const lotteryBehaviorSQL = `
WITH u AS (
  SELECT id, created_at, vip_expire_at, vip_lifetime_at, integral, experience
  FROM users WHERE id = $1 AND appid = $2
), online AS (
  SELECT
    COALESCE(SUM(seconds) FILTER (WHERE day >= $4::date - 6), 0)  AS s7,
    COALESCE(SUM(seconds) FILTER (WHERE day >= $4::date - 29), 0) AS s30,
    COUNT(*) FILTER (WHERE day >= $4::date - 29 AND seconds >= 60) AS d30,
    MAX(updated_at) AS last_at
  FROM user_online_daily WHERE user_id = $1 AND day >= $4::date - 29
), logins AS (
  SELECT COUNT(DISTINCT (created_at AT TIME ZONE 'UTC')::date) AS d30, MAX(created_at) AS last_at
  FROM login_audit_logs
  WHERE user_id = $1 AND appid = $2 AND status = 'success' AND created_at >= $3::timestamptz - INTERVAL '30 days'
)
SELECT
  EXTRACT(EPOCH FROM ($3::timestamptz - u.created_at)) / 86400,
  online.s7 / 60.0, online.s30 / 60.0, online.d30, logins.d30,
  GREATEST(logins.last_at, online.last_at,
           (SELECT MAX(last_seen_at) FROM user_login_devices WHERE appid = $2 AND user_id = $1)),
  COALESCE((SELECT CASE WHEN s.last_sign_date >= $4::date - 1 THEN s.consecutive_days ELSE 0 END
            FROM sign_stats s WHERE s.user_id = $1 AND s.appid = $2), 0),
  (SELECT COUNT(*) FROM daily_signins WHERE user_id = $1 AND appid = $2 AND sign_date >= $4::date - 29),
  COALESCE((SELECT total_sign_days FROM sign_stats WHERE user_id = $1 AND appid = $2), 0),
  EXISTS (SELECT 1 FROM daily_signins WHERE user_id = $1 AND appid = $2 AND sign_date = $4::date),
  COALESCE(u.vip_expire_at > $3::timestamptz, FALSE) OR u.vip_lifetime_at IS NOT NULL,
  u.vip_lifetime_at IS NOT NULL,
  COALESCE((SELECT SUM(duration_days) FROM vip_transactions WHERE user_id = $1 AND appid = $2), 0),
  COALESCE((SELECT SUM(amount) FROM payment_orders WHERE user_id = $1 AND appid = $2 AND status = 'paid'), 0),
  (SELECT COUNT(*) FROM payment_orders WHERE user_id = $1 AND appid = $2 AND status = 'paid'),
  u.experience,
  COALESCE((SELECT current_level FROM user_level_records WHERE user_id = $1 AND appid = $2), 1),
  u.integral,
  COALESCE((SELECT SUM(amount) FROM integral_transactions
            WHERE user_id = $1 AND appid = $2 AND amount > 0 AND created_at >= $3::timestamptz - INTERVAL '30 days'), 0),
  (SELECT COUNT(*) FROM invite_relations WHERE appid = $2 AND inviter_id = $1),
  (SELECT COUNT(*) FROM app_rewarded_ad_views
   WHERE appid = $2 AND user_id = $1 AND status = 'granted' AND created_at >= $3::timestamptz - INTERVAL '30 days'),
  (SELECT COUNT(*) FROM points_store_redemptions WHERE appid = $2 AND user_id = $1)
    + (SELECT COUNT(*) FROM card_key_redemptions WHERE appid = $2 AND user_id = $1),
  (SELECT COUNT(*) FROM user_login_devices WHERE appid = $2 AND user_id = $1),
  COALESCE((SELECT MAX(total_score) FROM risk_assessments
            WHERE user_id = $1 AND created_at >= $3::timestamptz - INTERVAL '30 days'), 0)
FROM u, online, logins`

// LotteryBehaviorProfile 这个人的行为画像。today 是应用时区的今天。
//
// 在抽奖事务之外取：都是只读的历史事实，不需要与抽奖串行，也不该让这一串查询占着用户行锁。
func (r *Repository) LotteryBehaviorProfile(ctx context.Context, appID, userID int64, now, today time.Time) (lotterydomain.BehaviorProfile, error) {
	var p lotterydomain.BehaviorProfile
	var lastActive *time.Time
	var paid float64
	var onlineDays, loginDays, signDays30, paidOrders, invites, ads, redemptions, devices int64
	var signStreak, signTotal, memberDays, experience, level, integral, earned, risk int64
	err := r.pool.QueryRow(ctx, lotteryBehaviorSQL, userID, appID, now, today.Format(time.DateOnly)).Scan(
		&p.RegisteredDays, &p.OnlineMinutes7d, &p.OnlineMinutes30d, &onlineDays, &loginDays, &lastActive,
		&signStreak, &signDays30, &signTotal, &p.SignedToday, &p.Member, &p.Lifetime, &memberDays, &paid, &paidOrders,
		&experience, &level, &integral, &earned, &invites, &ads, &redemptions, &devices, &risk,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return p, ErrUserNotFound
		}
		return p, err
	}
	p.RegisteredDays = math.Max(0, p.RegisteredDays)
	p.OnlineDays30d, p.LoginDays30d = float64(onlineDays), float64(loginDays)
	p.SignStreak, p.SignDays30d, p.SignTotalDays = float64(signStreak), float64(signDays30), float64(signTotal)
	p.MemberDays = math.Max(0, float64(memberDays))
	switch {
	case p.Lifetime:
		p.MemberRate = 1
	case p.RegisteredDays >= 1:
		p.MemberRate = math.Min(1, p.MemberDays/p.RegisteredDays)
	case p.Member:
		p.MemberRate = 1
	}
	p.PaidAmount, p.PaidOrders = paid, float64(paidOrders)
	p.Experience, p.Level, p.Integral = float64(experience), float64(level), float64(integral)
	p.PointsEarned30d, p.Invites, p.AdViews30d = float64(earned), float64(invites), float64(ads)
	p.Redemptions, p.Devices, p.RiskScore30d = float64(redemptions), float64(devices), float64(risk)
	if lastActive != nil {
		p.RecencyDays = math.Max(0, now.Sub(*lastActive).Hours()/24)
	} else {
		p.RecencyDays = p.RegisteredDays
	}
	return p, nil
}

// ── 抽奖时的状态 ──

// lotteryDrawState 选奖所需的、本活动里的事实（在抽奖事务里、锁用户行之后取）。
// 只查策略用得到的部分：没开风控就不数同设备账号，没开预算就不算今天发了多少积分。
func lotteryDrawState(ctx context.Context, tx queryExecutor, activity lotterydomain.Activity, prizes []lotterydomain.Prize,
	input lotterydomain.DrawInput, facts lotterydomain.UserFacts, now time.Time) (lotterydomain.DrawState, error) {
	strategy := activity.Strategy
	state := lotterydomain.DrawState{
		Now: now, Source: input.Source, MissStreak: facts.MissStreak, TotalDraws: int(facts.TotalDraws),
		PrizeWins: map[int64]int{}, PrizeWinsToday: map[int64]int{}, PrizeDailyUsed: map[int64]int64{},
		DeviceAccounts: 1, IPAccounts: 1, Profile: input.Profile,
	}
	if input.Location != nil {
		state.Now = now.In(input.Location)
	}
	if !input.DayStart.IsZero() {
		state.DayProgress = math.Min(1, math.Max(0, now.Sub(input.DayStart).Hours()/24))
	}
	if err := tx.QueryRow(ctx, `SELECT luck_expected, luck_variance, luck_skew FROM lottery_user_states
WHERE activity_id = $1 AND user_id = $2`, activity.ID, input.UserID).Scan(&state.LuckExpected, &state.LuckVariance,
		&state.LuckSkew); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return state, err
	}
	if strategy.WinRate != nil {
		if err := tx.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE prize_kind <> 'none')
FROM lottery_draws WHERE activity_id = $1 AND created_at >= $2`, activity.ID, input.DayStart).
			Scan(&state.ActivityDrawsToday, &state.ActivityWinsToday); err != nil {
			return state, err
		}
	}

	// 本活动里这个人每个奖品中过几次、今天抽了几次
	rows, err := tx.Query(ctx, `SELECT prize_id, prize_kind <> 'none',
  COUNT(*), COUNT(*) FILTER (WHERE created_at >= $3)
FROM lottery_draws WHERE activity_id = $1 AND user_id = $2
GROUP BY prize_id, prize_kind <> 'none'`, activity.ID, input.UserID, input.DayStart)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var prizeID *int64
		var winning bool
		var total, today int
		if err := rows.Scan(&prizeID, &winning, &total, &today); err != nil {
			rows.Close()
			return state, err
		}
		state.TodayDraws += today
		if winning {
			state.WinsTotal += total
			state.WinsToday += today
			if prizeID != nil {
				state.PrizeWins[*prizeID] += total
				state.PrizeWinsToday[*prizeID] += today
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return state, err
	}

	if c := strategy.Cooldown; c != nil {
		rows, err := tx.Query(ctx, `SELECT CASE WHEN prize_kind = 'none' THEN 0 ELSE COALESCE(prize_id, 0) END
FROM lottery_draws WHERE activity_id = $1 AND user_id = $2 ORDER BY created_at DESC, id DESC LIMIT $3`,
			activity.ID, input.UserID, c.Draws)
		if err != nil {
			return state, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return state, err
			}
			state.Recent = append(state.Recent, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return state, err
		}
	}

	limited := make([]int64, 0, len(prizes))
	for _, prize := range prizes {
		if prize.DailyLimit > 0 {
			limited = append(limited, prize.ID)
		}
	}
	if len(limited) > 0 {
		rows, err := tx.Query(ctx, `SELECT prize_id, used FROM lottery_prize_daily
WHERE prize_id = ANY($1) AND day_start = $2`, limited, input.DayStart)
		if err != nil {
			return state, err
		}
		for rows.Next() {
			var id, used int64
			if err := rows.Scan(&id, &used); err != nil {
				rows.Close()
				return state, err
			}
			state.PrizeDailyUsed[id] = used
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return state, err
		}
	}

	if strategy.Budget != nil {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(CASE
    WHEN conversion IS NOT NULL THEN
      CASE WHEN conversion->'reward'->>'type' = 'points' THEN (conversion->'reward'->>'amount')::bigint ELSE 0 END
    WHEN reward->>'type' = 'points' THEN (reward->>'amount')::bigint
    ELSE 0 END), 0)
FROM lottery_draws WHERE activity_id = $1 AND created_at >= $2`, activity.ID, input.DayStart).
			Scan(&state.PointsIssuedToday); err != nil {
			return state, err
		}
	}

	if err := lotteryRiskFacts(ctx, tx, strategy.Risk, activity.ID, input, now, &state); err != nil {
		return state, err
	}
	return state, nil
}

// lotteryRiskFacts 风控规则要用的事实。只数生效规则用得到的信号；聚集类数的是除了自己之外的账号，再加上自己。
func lotteryRiskFacts(ctx context.Context, tx queryExecutor, guard *lotterydomain.RiskGuard, activityID int64,
	input lotterydomain.DrawInput, now time.Time, state *lotterydomain.DrawState) error {
	state.DeviceMissing = input.DeviceID == ""
	state.IPMissing = input.ClientIP == ""
	if guard == nil {
		return nil
	}
	uses := func(signal string) bool { return guard.Uses(signal) }
	if !state.DeviceMissing && uses(lotterydomain.RiskSignalDeviceAccounts) {
		if err := tx.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) + 1 FROM lottery_draws
WHERE activity_id = $1 AND device_id = $2 AND created_at >= $3 AND user_id <> $4`,
			activityID, input.DeviceID, input.DayStart, input.UserID).Scan(&state.DeviceAccounts); err != nil {
			return err
		}
	}
	if !state.IPMissing && uses(lotterydomain.RiskSignalIPAccounts) {
		if err := tx.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) + 1 FROM lottery_draws
WHERE activity_id = $1 AND client_ip = $2 AND created_at >= $3 AND user_id <> $4`,
			activityID, input.ClientIP, input.DayStart, input.UserID).Scan(&state.IPAccounts); err != nil {
			return err
		}
	}
	if subnet := lotterydomain.SubnetOf(input.ClientIP); subnet != "" && uses(lotterydomain.RiskSignalSubnetAccounts) {
		// client_ip 是文本：先判能不能解析成 inet，再按族取 /24 或 /64（与 lottery.SubnetOf 同一口径）
		if err := tx.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) + 1 FROM lottery_draws
WHERE activity_id = $1 AND created_at >= $2 AND user_id <> $3 AND client_ip IS NOT NULL
  AND CASE WHEN pg_input_is_valid(client_ip, 'inet')
        THEN network(set_masklen(client_ip::inet, CASE WHEN family(client_ip::inet) = 4 THEN 24 ELSE 64 END))
      END = $4::cidr`, activityID, input.DayStart, input.UserID, subnet).Scan(&state.SubnetAccounts); err != nil {
			return err
		}
	} else if subnet == "" && !state.IPMissing {
		state.SubnetAccounts = 1
	}
	if !state.DeviceMissing && uses(lotterydomain.RiskSignalDeviceHistory) {
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM user_login_devices WHERE appid = $1 AND device_key = $2`,
			input.AppID, input.DeviceID).Scan(&state.DeviceHistory); err != nil {
			return err
		}
	}
	if uses(lotterydomain.RiskSignalBurst) {
		since := now.Add(-lotterydomain.BurstWindowMinutes * time.Minute)
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draws
WHERE activity_id = $1 AND user_id = $2 AND created_at >= $3`, activityID, input.UserID, since).Scan(&state.RecentDraws); err != nil {
			return err
		}
	}
	return nil
}

// reserveLotteryPrize 给选中的奖品占一份：先占全站每日限量，再扣库存。
// 两步都是条件更新（不超过上限才 +1），任何一步被并发抢光都返回 false，调用方改发兜底奖；
// 库存没扣到时把刚占的每日名额还回去。不限量、不限每日的奖品什么都不写。
func reserveLotteryPrize(ctx context.Context, tx pgx.Tx, prize lotterydomain.Prize, dayStart time.Time) (bool, error) {
	if prize.DailyLimit > 0 {
		tag, err := tx.Exec(ctx, `INSERT INTO lottery_prize_daily (prize_id, day_start, used) VALUES ($1, $2, 1)
ON CONFLICT (prize_id, day_start) DO UPDATE SET used = lottery_prize_daily.used + 1
WHERE lottery_prize_daily.used < $3`, prize.ID, dayStart, prize.DailyLimit)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 0 {
			return false, nil
		}
	}
	if prize.Quantity < 0 {
		return true, nil
	}
	tag, err := tx.Exec(ctx, `UPDATE lottery_prizes SET used = used + 1, updated_at = NOW()
WHERE id = $1 AND quantity >= 0 AND used < quantity`, prize.ID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	if prize.DailyLimit > 0 {
		if _, err := tx.Exec(ctx, `UPDATE lottery_prize_daily SET used = used - 1
WHERE prize_id = $1 AND day_start = $2 AND used > 0`, prize.ID, dayStart); err != nil {
			return false, err
		}
	}
	return false, nil
}

// ── 在线时长 ──

// AccrueOnlineSeconds 把 [from, to) 这段在线时间按 UTC 自然日记进 user_online_daily。
// 同一个人多个设备同时在线会各记一份，单日封顶 86400 秒。
func (r *Repository) AccrueOnlineSeconds(ctx context.Context, appID, userID int64, from, to time.Time) error {
	from, to = from.UTC(), to.UTC()
	for from.Before(to) {
		dayEnd := time.Date(from.Year(), from.Month(), from.Day()+1, 0, 0, 0, 0, time.UTC)
		end := to
		if dayEnd.Before(end) {
			end = dayEnd
		}
		seconds := int64(end.Sub(from) / time.Second)
		if seconds > 0 {
			if _, err := r.pool.Exec(ctx, `INSERT INTO user_online_daily (appid, user_id, day, seconds)
VALUES ($1, $2, $3, LEAST($4, 86400))
ON CONFLICT (user_id, day) DO UPDATE SET seconds = LEAST(user_online_daily.seconds + EXCLUDED.seconds, 86400),
  updated_at = NOW()`, appID, userID, from.Format(time.DateOnly), seconds); err != nil {
				return err
			}
		}
		from = end
	}
	return nil
}

// LotteryPreview 管理端预览：这个人此刻再抽一次时的出奖方案（不加锁、不抽、不写）。
// 预览没有请求上下文：设备与 IP 取这个人在应用里最近一次抽奖用的，风控按它们评估；一次都没抽过时聚集按 1 个账号算。
func (r *Repository) LotteryPreview(ctx context.Context, activity lotterydomain.Activity, prizes []lotterydomain.Prize,
	appID, userID int64, profile lotterydomain.BehaviorProfile, now, dayStart time.Time, location *time.Location,
) (lotterydomain.DrawState, error) {
	facts, err := lotteryUserFacts(ctx, r.pool, appID, activity.ID, userID, dayStart, false)
	if err != nil {
		return lotterydomain.DrawState{}, err
	}
	input := lotterydomain.DrawInput{
		AppID: appID, UserID: userID, ActivityID: activity.ID, DayStart: dayStart, Now: now, Location: location,
		Profile: profile,
	}
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(device_id, ''), COALESCE(client_ip, '') FROM lottery_draws
WHERE appid = $1 AND user_id = $2 ORDER BY created_at DESC LIMIT 1`, appID, userID).Scan(&input.DeviceID, &input.ClientIP); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return lotterydomain.DrawState{}, err
	}
	state, err := lotteryDrawState(ctx, r.pool, activity, prizes, input, facts, now)
	if err != nil {
		return state, err
	}
	if input.DeviceID == "" && input.ClientIP == "" {
		state.DeviceMissing, state.IPMissing = false, false
	}
	chances := lotterydomain.EvaluateChances(activity, facts, lotterydomain.VideoChances{})
	state.Source = chances.Next
	return state, nil
}

// MaxLotteryAuditDraws 一次审计最多读多少条记录（最近的优先）。
const MaxLotteryAuditDraws = 200_000

// LotteryAuditDraws 一个活动在 since 之后带出奖方案的记录（最近的优先，最多 MaxLotteryAuditDraws 条）。
func (r *Repository) LotteryAuditDraws(ctx context.Context, appID, activityID int64, since time.Time) ([]lotterydomain.AuditDraw, error) {
	rows, err := r.pool.Query(ctx, `SELECT d.trace FROM lottery_draws d
JOIN lottery_activities a ON a.id = d.activity_id AND a.appid = $1
WHERE d.activity_id = $2 AND d.created_at >= $3 AND d.trace IS NOT NULL
ORDER BY d.id DESC LIMIT $4`, appID, activityID, since, MaxLotteryAuditDraws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]lotterydomain.AuditDraw, 0, 1024)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var plan lotterydomain.DrawPlan
		if err := json.Unmarshal(raw, &plan); err != nil || len(plan.Odds) == 0 {
			continue
		}
		out = append(out, lotterydomain.AuditDraw{Odds: plan.Odds, Picked: plan.Picked, Pity: plan.Pity, Fallback: plan.Fallback})
	}
	return out, rows.Err()
}
