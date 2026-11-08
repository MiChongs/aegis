package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	lotterydomain "aegis/internal/domain/lottery"
)

// 抽奖的计数（迁移 000149，docs/lottery.md「容量与并发」）。
//
// 抽奖事务里要用的事实全部是 O(1) 读出来的：
//   - 这个人的：lottery_user_states 一行（今天的免费 / 积分 / 抽奖 / 中奖次数、每个奖品中过几次、
//     最近抽中的奖品、近 10 分钟抽了几次），在锁内读、在锁内写；
//   - 全站的：每次抽奖在事务里追加一行增量（lottery_activity_deltas），后台原子地并进 lottery_activity_daily；
//   - 风控聚集：lottery_risk_keys 每台设备 / 每个 IP / 每个网段当天抽过奖的账号，按主键范围数。
//
// 不再对 lottery_draws 做任何聚合。记录照常写，只用于展示、检索与统计。

// lotteryRecentCap 状态行里最多留多少个最近抽中的奖品（= 中奖冷却允许的最大次数）。
const lotteryRecentCap = lotterydomain.MaxCooldownDraws

// lotteryBurstWindow 近 N 分钟抽了几次的窗口（与风控的 BurstWindowMinutes 一致）。
const lotteryBurstWindow = lotterydomain.BurstWindowMinutes * time.Minute

// 风控聚集键的种类（lottery_risk_keys.kind）。
const (
	lotteryRiskKeyDevice int16 = 1
	lotteryRiskKeyIP     int16 = 2
	lotteryRiskKeySubnet int16 = 3
)

// lotteryRiskKeyRetention 风控聚集键保留几天（只用当天的，多留两天备查）。
const lotteryRiskKeyRetention = 3 * 24 * time.Hour

// lotteryCounters 一个人在一个活动里的状态行。
type lotteryCounters struct {
	Exists bool

	MissStreak int
	TotalDraws int64
	// Counted 计数里已经算进去的抽奖次数；与 TotalDraws 不等时计数不可信，要按记录重算
	Counted    int64
	LastDrawAt *time.Time

	LuckExpected float64
	LuckVariance float64
	LuckSkew     float64

	// DayStart 「今天」计数属于哪一天；与当前的今天不同时今天的计数按 0 算
	DayStart       *time.Time
	FreeToday      int
	PointsToday    int
	DrawsToday     int
	WinsToday      int
	WinsTotal      int64
	PrizeWins      map[int64]int
	PrizeWinsToday map[int64]int
	// Recent 最近抽中的奖品 ID，新的在前，未中记 0
	Recent []int64

	BurstStart *time.Time
	BurstCount int
	BurstPrev  int
}

const lotteryCounterColumns = `miss_streak, total_draws, counted_draws, last_draw_at, luck_expected, luck_variance, luck_skew,
  day_start, free_today, points_today, draws_today, wins_today, wins_total, prize_wins, prize_wins_today, recent,
  burst_start, burst_count, burst_prev`

func scanLotteryCounters(row pgx.Row) (lotteryCounters, error) {
	var c lotteryCounters
	var wins, winsToday []byte
	if err := row.Scan(&c.MissStreak, &c.TotalDraws, &c.Counted, &c.LastDrawAt, &c.LuckExpected, &c.LuckVariance,
		&c.LuckSkew, &c.DayStart, &c.FreeToday, &c.PointsToday, &c.DrawsToday, &c.WinsToday, &c.WinsTotal, &wins,
		&winsToday, &c.Recent, &c.BurstStart, &c.BurstCount, &c.BurstPrev); err != nil {
		return c, err
	}
	c.Exists = true
	c.PrizeWins = decodePrizeCounts(wins)
	c.PrizeWinsToday = decodePrizeCounts(winsToday)
	return c, nil
}

func decodePrizeCounts(raw []byte) map[int64]int {
	out := map[int64]int{}
	if len(raw) == 0 {
		return out
	}
	var byKey map[string]int
	if err := json.Unmarshal(raw, &byKey); err != nil {
		return out
	}
	for key, count := range byKey {
		if id, err := strconv.ParseInt(key, 10, 64); err == nil && count > 0 {
			out[id] = count
		}
	}
	return out
}

func encodePrizeCounts(counts map[int64]int) []byte {
	byKey := make(map[string]int, len(counts))
	for id, count := range counts {
		if count > 0 {
			byKey[strconv.FormatInt(id, 10)] = count
		}
	}
	raw, _ := json.Marshal(byKey)
	return raw
}

func emptyLotteryCounters() lotteryCounters {
	return lotteryCounters{Counted: 0, PrizeWins: map[int64]int{}, PrizeWinsToday: map[int64]int{}}
}

// trusted 计数是否与记录对得上（新加的列、滚动发布期间旧版本抽过时对不上）。
func (c lotteryCounters) trusted() bool { return !c.Exists || c.Counted == c.TotalDraws }

// today 把「今天」的计数对齐到 dayStart：存的是别的日子时按 0 算。
func (c lotteryCounters) today(dayStart time.Time) lotteryCounters {
	if c.DayStart != nil && c.DayStart.Equal(dayStart) {
		return c
	}
	c.FreeToday, c.PointsToday, c.DrawsToday, c.WinsToday = 0, 0, 0, 0
	c.PrizeWinsToday = map[int64]int{}
	day := dayStart
	c.DayStart = &day
	return c
}

// recentDraws 近 10 分钟抽了几次（滑动窗口：本窗口的次数 + 上一窗口按未过去的比例折算）。
func (c lotteryCounters) recentDraws(now time.Time) int {
	if c.BurstStart == nil {
		return 0
	}
	current := now.Truncate(lotteryBurstWindow)
	rest := 1 - float64(now.Sub(current))/float64(lotteryBurstWindow)
	switch {
	case c.BurstStart.Equal(current):
		return c.BurstCount + int(math.Round(float64(c.BurstPrev)*rest))
	case c.BurstStart.Equal(current.Add(-lotteryBurstWindow)):
		return int(math.Round(float64(c.BurstCount) * rest))
	}
	return 0
}

// advance 记上刚抽完的这一次。prize 是抽中的格子（不是改发后的奖励），与记录里的 prize_id / prize_kind 同一口径。
func (c lotteryCounters) advance(dayStart, now time.Time, source string, prize lotterydomain.Prize, plan lotterydomain.DrawPlan,
	missStreak int) lotteryCounters {
	c = c.today(dayStart)
	c.TotalDraws++
	c.Counted = c.TotalDraws
	c.DrawsToday++
	switch source {
	case lotterydomain.SourceFree:
		c.FreeToday++
	case lotterydomain.SourcePoints:
		c.PointsToday++
	}
	var picked int64
	if prize.Kind != lotterydomain.KindNone {
		picked = prize.ID
		c.WinsTotal++
		c.WinsToday++
		wins := make(map[int64]int, len(c.PrizeWins)+1)
		for id, n := range c.PrizeWins {
			wins[id] = n
		}
		winsToday := make(map[int64]int, len(c.PrizeWinsToday)+1)
		for id, n := range c.PrizeWinsToday {
			winsToday[id] = n
		}
		wins[prize.ID]++
		winsToday[prize.ID]++
		c.PrizeWins, c.PrizeWinsToday = wins, winsToday
	}
	recent := make([]int64, 0, min(len(c.Recent)+1, lotteryRecentCap))
	recent = append(recent, picked)
	for _, id := range c.Recent {
		if len(recent) >= lotteryRecentCap {
			break
		}
		recent = append(recent, id)
	}
	c.Recent = recent

	current := now.Truncate(lotteryBurstWindow)
	switch {
	case c.BurstStart != nil && c.BurstStart.Equal(current):
		c.BurstCount++
	case c.BurstStart != nil && c.BurstStart.Equal(current.Add(-lotteryBurstWindow)):
		c.BurstPrev, c.BurstCount = c.BurstCount, 1
	default:
		c.BurstPrev, c.BurstCount = 0, 1
	}
	c.BurstStart = &current

	c.MissStreak = missStreak
	at := now
	c.LastDrawAt = &at
	fair := plan.Fair
	c.LuckExpected += fair
	c.LuckVariance += fair * (1 - fair)
	c.LuckSkew += fair * (1 - fair) * (1 - 2*fair)
	return c
}

// lockLotteryCountersTx 锁住并读出这个人在活动里的状态行（没有就建）。计数对不上记录时在锁内按记录重算。
func lockLotteryCountersTx(ctx context.Context, tx pgx.Tx, activityID, userID int64, dayStart, now time.Time) (lotteryCounters, error) {
	read := func() (lotteryCounters, error) {
		return scanLotteryCounters(tx.QueryRow(ctx, `SELECT `+lotteryCounterColumns+` FROM lottery_user_states
WHERE activity_id = $1 AND user_id = $2 FOR UPDATE`, activityID, userID))
	}
	counters, err := read()
	if errors.Is(err, pgx.ErrNoRows) {
		// 新行的 counted_draws 是 -1：第一次抽奖按记录核一次（新用户没有记录，几个索引点查；
		// 早年有记录却没有状态行的旧数据也能对上）
		if _, err := tx.Exec(ctx, `INSERT INTO lottery_user_states (activity_id, user_id) VALUES ($1, $2)
ON CONFLICT (activity_id, user_id) DO NOTHING`, activityID, userID); err != nil {
			return counters, err
		}
		counters, err = read()
	}
	if err != nil {
		return counters, err
	}
	if !counters.trusted() {
		return recountLotteryCounters(ctx, tx, counters, activityID, userID, dayStart, now)
	}
	return counters, nil
}

// readLotteryCounters 不加锁地读这个人的状态行（活动页、预览用）。计数对不上记录时按记录重算，
// 并在没有人同时抽奖的前提下（total_draws 没变）顺手写回，下次就不用再算。
func (r *Repository) readLotteryCounters(ctx context.Context, activityID, userID int64, dayStart, now time.Time) (lotteryCounters, error) {
	counters, err := scanLotteryCounters(r.pool.QueryRow(ctx, `SELECT `+lotteryCounterColumns+` FROM lottery_user_states
WHERE activity_id = $1 AND user_id = $2`, activityID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return emptyLotteryCounters(), nil
	}
	if err != nil {
		return counters, err
	}
	if counters.trusted() {
		return counters, nil
	}
	seen := counters.TotalDraws
	counters, err = recountLotteryCounters(ctx, r.pool, counters, activityID, userID, dayStart, now)
	if err != nil {
		return counters, err
	}
	_ = writeLotteryCounters(ctx, r.pool, activityID, userID, counters,
		` AND counted_draws <> total_draws AND total_draws = `+strconv.FormatInt(seen, 10))
	return counters, nil
}

// recountLotteryCounters 按记录重算这个人的计数（每人一次：新加列之后的第一次，或滚动发布期间旧版本抽过之后）。
// 连续未中与运气累计不从记录推：它们一直由状态行维护（旧版本也在写）。
func recountLotteryCounters(ctx context.Context, q queryExecutor, c lotteryCounters, activityID, userID int64,
	dayStart, now time.Time) (lotteryCounters, error) {
	current := now.Truncate(lotteryBurstWindow)
	previous := current.Add(-lotteryBurstWindow)
	var total, wins int64
	if err := q.QueryRow(ctx, `SELECT
  COUNT(*),
  COUNT(*) FILTER (WHERE prize_kind <> 'none'),
  COUNT(*) FILTER (WHERE created_at >= $3 AND source = 'free'),
  COUNT(*) FILTER (WHERE created_at >= $3 AND source = 'points'),
  COUNT(*) FILTER (WHERE created_at >= $3),
  COUNT(*) FILTER (WHERE created_at >= $3 AND prize_kind <> 'none'),
  COUNT(*) FILTER (WHERE created_at >= $4),
  COUNT(*) FILTER (WHERE created_at >= $5 AND created_at < $4)
FROM lottery_draws WHERE activity_id = $1 AND user_id = $2`, activityID, userID, dayStart, current, previous).Scan(
		&total, &wins, &c.FreeToday, &c.PointsToday, &c.DrawsToday, &c.WinsToday, &c.BurstCount, &c.BurstPrev); err != nil {
		return c, err
	}
	c.TotalDraws, c.Counted, c.WinsTotal = total, total, wins
	day := dayStart
	c.DayStart = &day
	c.BurstStart = &current

	c.PrizeWins, c.PrizeWinsToday = map[int64]int{}, map[int64]int{}
	rows, err := q.Query(ctx, `SELECT prize_id, COUNT(*), COUNT(*) FILTER (WHERE created_at >= $3)
FROM lottery_draws WHERE activity_id = $1 AND user_id = $2 AND prize_kind <> 'none' AND prize_id IS NOT NULL
GROUP BY prize_id`, activityID, userID, dayStart)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var id int64
		var all, today int
		if err := rows.Scan(&id, &all, &today); err != nil {
			rows.Close()
			return c, err
		}
		c.PrizeWins[id] = all
		if today > 0 {
			c.PrizeWinsToday[id] = today
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return c, err
	}

	c.Recent = make([]int64, 0, 8)
	rows, err = q.Query(ctx, `SELECT CASE WHEN prize_kind = 'none' THEN 0 ELSE COALESCE(prize_id, 0) END
FROM lottery_draws WHERE activity_id = $1 AND user_id = $2 ORDER BY created_at DESC, id DESC LIMIT $3`,
		activityID, userID, lotteryRecentCap)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return c, err
		}
		c.Recent = append(c.Recent, id)
	}
	rows.Close()
	return c, rows.Err()
}

// lotteryCountersUpdateSQL 写回状态行（$1 活动、$2 用户，其余按列顺序）。
const lotteryCountersUpdateSQL = `UPDATE lottery_user_states SET
  miss_streak = $3, total_draws = $4, counted_draws = $5, last_draw_at = $6,
  luck_expected = $7, luck_variance = $8, luck_skew = $9,
  day_start = $10, free_today = $11, points_today = $12, draws_today = $13, wins_today = $14, wins_total = $15,
  prize_wins = $16, prize_wins_today = $17, recent = $18, burst_start = $19, burst_count = $20, burst_prev = $21,
  updated_at = NOW()
WHERE activity_id = $1 AND user_id = $2`

// writeLotteryCounters 写回状态行。guard 是附加在 WHERE 后面的条件（不加锁的写回用）。
func writeLotteryCounters(ctx context.Context, q queryExecutor, activityID, userID int64, c lotteryCounters, guard string) error {
	recent := c.Recent
	if recent == nil {
		recent = []int64{}
	}
	_, err := q.Exec(ctx, lotteryCountersUpdateSQL+guard, activityID, userID, c.MissStreak, c.TotalDraws, c.Counted,
		c.LastDrawAt, c.LuckExpected, c.LuckVariance, c.LuckSkew, c.DayStart, c.FreeToday, c.PointsToday, c.DrawsToday,
		c.WinsToday, c.WinsTotal, encodePrizeCounts(c.PrizeWins), encodePrizeCounts(c.PrizeWinsToday), recent,
		c.BurstStart, c.BurstCount, c.BurstPrev)
	return err
}

// ── 全站计数 ──
//
// 全站计数（全站中奖率反馈、积分预算）的每一次增量都随抽奖事务落库：事务里往 lottery_activity_deltas 追加一行
// （只插入，并发的事务互不等锁）。后台每 500 ms 把已提交的增量在一条语句里删掉并加进 lottery_activity_daily
// （CompactLotteryActivityDeltas，原子：删多少就加多少，多个实例同时合并时用 SKIP LOCKED 各取各的）。
// 读的时候 = 日计数 + 还没合并的增量，按活动、按天缓存 250 ms（只缓存读；进程崩溃丢的只是缓存）。

// lotteryTotalsTTL 全站计数读缓存的有效期。
const lotteryTotalsTTL = 250 * time.Millisecond

// lotteryCompactBatch 一次合并最多多少行增量。
const lotteryCompactBatch = 20_000

type lotteryTotalsKey struct {
	activityID int64
	day        int64 // dayStart 的 Unix 秒
}

type lotteryTotals struct{ draws, wins, points int64 }

func (t lotteryTotals) plus(o lotteryTotals) lotteryTotals {
	return lotteryTotals{draws: t.draws + o.draws, wins: t.wins + o.wins, points: t.points + o.points}
}

type lotteryTotalsEntry struct {
	at   time.Time
	base lotteryTotals
	// local 缓存之后本进程又提交了的（让本进程连抽时读到自己刚抽的；下次刷新时库里已包含，清零）
	local lotteryTotals
}

// lotteryTotalsCache 全站计数的读缓存（Repository 的字段，零值可用）。
type lotteryTotalsCache struct {
	mu      sync.Mutex
	entries map[lotteryTotalsKey]*lotteryTotalsEntry
}

// queueLotteryActivityDelta 这一次抽奖的全站增量（入批，随抽奖事务提交）。
func queueLotteryActivityDelta(p *pgPipe, activityID int64, dayStart time.Time, win bool, points int64) {
	var wins int
	if win {
		wins = 1
	}
	p.exec(`INSERT INTO lottery_activity_deltas (activity_id, day_start, draws, wins, points_issued)
VALUES ($1, $2, 1, $3, $4)`, activityID, dayStart, wins, points)
}

// noteLotteryDraw 抽奖提交之后，把这一次加到本进程的读缓存上（不是计数本身，计数已随事务落库）。
func (r *Repository) noteLotteryDraw(activityID int64, dayStart time.Time, win bool, points int64) {
	delta := lotteryTotals{draws: 1, points: points}
	if win {
		delta.wins = 1
	}
	c := &r.lotteryTotals
	c.mu.Lock()
	if e := c.entries[lotteryTotalsKey{activityID: activityID, day: dayStart.Unix()}]; e != nil {
		e.local = e.local.plus(delta)
	}
	c.mu.Unlock()
}

// lotteryActivityTotalsSQL 本活动某一天的全站计数：日计数 + 还没合并的增量（$1 活动、$2 当天零点）。
const lotteryActivityTotalsSQL = `SELECT
  COALESCE((SELECT SUM(draws) FROM lottery_activity_daily WHERE activity_id = $1 AND day_start = $2), 0)
    + COALESCE((SELECT SUM(draws) FROM lottery_activity_deltas WHERE activity_id = $1 AND day_start = $2), 0),
  COALESCE((SELECT SUM(wins) FROM lottery_activity_daily WHERE activity_id = $1 AND day_start = $2), 0)
    + COALESCE((SELECT SUM(wins) FROM lottery_activity_deltas WHERE activity_id = $1 AND day_start = $2), 0),
  COALESCE((SELECT SUM(points_issued) FROM lottery_activity_daily WHERE activity_id = $1 AND day_start = $2), 0)
    + COALESCE((SELECT SUM(points_issued) FROM lottery_activity_deltas WHERE activity_id = $1 AND day_start = $2), 0)`

// cachedLotteryTotals 读缓存里还新鲜的全站计数（含本进程缓存之后又提交的）。
func (r *Repository) cachedLotteryTotals(activityID int64, dayStart time.Time) (lotteryTotals, bool) {
	c := &r.lotteryTotals
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[lotteryTotalsKey{activityID: activityID, day: dayStart.Unix()}]; e != nil &&
		time.Since(e.at) < lotteryTotalsTTL {
		return e.base.plus(e.local), true
	}
	return lotteryTotals{}, false
}

// storeLotteryTotals 刚从库里读到的全站计数放进读缓存（顺手清掉一分钟前的）。
func (r *Repository) storeLotteryTotals(activityID int64, dayStart time.Time, base lotteryTotals) {
	c := &r.lotteryTotals
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[lotteryTotalsKey]*lotteryTotalsEntry{}
	}
	for k, e := range c.entries {
		if now.Sub(e.at) > time.Minute {
			delete(c.entries, k)
		}
	}
	c.entries[lotteryTotalsKey{activityID: activityID, day: dayStart.Unix()}] = &lotteryTotalsEntry{at: now, base: base}
}

// CompactLotteryActivityDeltas 把已提交的增量并进日计数，返回并了多少行。一条语句里删掉、加上，原子；
// 多个实例同时合并时 SKIP LOCKED 各取各的，同一行增量只会被并一次。
func (r *Repository) CompactLotteryActivityDeltas(ctx context.Context) (int64, error) {
	var moved int64
	err := r.pool.QueryRow(ctx, `WITH moved AS (
  DELETE FROM lottery_activity_deltas
  WHERE id IN (SELECT id FROM lottery_activity_deltas ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED)
  RETURNING activity_id, day_start, draws, wins, points_issued
), merged AS (
  INSERT INTO lottery_activity_daily (activity_id, day_start, shard, draws, wins, points_issued)
  SELECT activity_id, day_start, 0, SUM(draws), SUM(wins), SUM(points_issued)
  FROM moved GROUP BY activity_id, day_start ORDER BY activity_id, day_start
  ON CONFLICT (activity_id, day_start, shard) DO UPDATE SET
    draws = lottery_activity_daily.draws + EXCLUDED.draws,
    wins = lottery_activity_daily.wins + EXCLUDED.wins,
    points_issued = lottery_activity_daily.points_issued + EXCLUDED.points_issued
  RETURNING 1
)
SELECT COUNT(*) FROM moved`, lotteryCompactBatch).Scan(&moved)
	return moved, err
}

// lotteryIssuedPoints 这一次发出去的积分（含已拥有折算），与积分预算同一口径。
func lotteryIssuedPoints(prize lotterydomain.Prize, conversion *lotterydomain.Conversion) int64 {
	if conversion != nil {
		if conversion.Reward.Type == lotterydomain.KindPoints {
			return conversion.Reward.Amount
		}
		return 0
	}
	if prize.Reward.Type == lotterydomain.KindPoints {
		return prize.Reward.Amount
	}
	return 0
}

// ── 风控聚集 ──

// lotteryRiskKeysNeeded 活动的风控规则用不用得到聚集类信号（用不到就不记）。
func lotteryRiskKeysNeeded(guard *lotterydomain.RiskGuard) bool {
	return guard != nil && (guard.Uses(lotterydomain.RiskSignalDeviceAccounts) ||
		guard.Uses(lotterydomain.RiskSignalIPAccounts) || guard.Uses(lotterydomain.RiskSignalSubnetAccounts))
}

// lotteryRiskAccountsSQL 本活动当天在这个键上抽过奖的其他账号数 + 1（自己）（$1 活动、$2 当天、$3 种类、$4 键、$5 本人）。
const lotteryRiskAccountsSQL = `SELECT COUNT(*) + 1 FROM lottery_risk_keys
WHERE activity_id = $1 AND day_start = $2 AND kind = $3 AND key = $4 AND user_id <> $5`

// queueLotteryRiskKeys 记下这个人今天在本活动用过的设备、IP、网段（入批，随抽奖事务提交）。
func queueLotteryRiskKeys(p *pgPipe, activityID, userID int64, dayStart time.Time, deviceID, clientIP string) {
	p.exec(`INSERT INTO lottery_risk_keys (activity_id, day_start, kind, key, user_id)
SELECT $1, $2, k.kind, k.key, $3 FROM (VALUES ($4::smallint, $5::text), ($6::smallint, $7::text), ($8::smallint, $9::text)) AS k(kind, key)
WHERE k.key <> ''
ON CONFLICT DO NOTHING`, activityID, dayStart, userID, lotteryRiskKeyDevice, truncateColumn(deviceID, 128),
		lotteryRiskKeyIP, truncateColumn(clientIP, 64), lotteryRiskKeySubnet, lotterydomain.SubnetOf(clientIP))
}

// PurgeLotteryRiskKeys 删掉过期的风控聚集键，返回删了多少行。
func (r *Repository) PurgeLotteryRiskKeys(ctx context.Context, now time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM lottery_risk_keys WHERE day_start < $1`, now.Add(-lotteryRiskKeyRetention))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 首次启动的补齐 ──

// lotteryCountersBackfill 补齐的名字（schema_backfills.name）。
const lotteryCountersBackfill = "lottery_counters_v1"

// lotteryIssuedPointsSQL 一条记录发出去的积分（与 lotteryIssuedPoints 同一口径）。
const lotteryIssuedPointsSQL = `CASE
    WHEN conversion IS NOT NULL THEN
      CASE WHEN conversion->'reward'->>'type' = 'points' THEN (conversion->'reward'->>'amount')::bigint ELSE 0 END
    WHEN reward->>'type' = 'points' THEN (reward->>'amount')::bigint
    ELSE 0 END`

// BackfillLotteryCounters 首次启动时从历史记录补出全站每天的计数（分片 -1）与今天的风控聚集键。
// 只做一次（schema_backfills 记一笔）；多实例同时启动时用咨询锁串行，后到的看到记录直接返回。
// timezone 是应用时区（与抽奖的「今天」同一个），dayStart 是该时区的今天零点。返回是否真的补了。
func (r *Repository) BackfillLotteryCounters(ctx context.Context, timezone string, dayStart time.Time) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('statement_timeout', '0', true), pg_advisory_xact_lock(hashtextextended($1, 0))`,
		lotteryCountersBackfill); err != nil {
		return false, err
	}
	var done bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_backfills WHERE name = $1)`, lotteryCountersBackfill).
		Scan(&done); err != nil || done {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO lottery_activity_daily (activity_id, day_start, shard, draws, wins, points_issued)
SELECT d.activity_id, (date_trunc('day', d.created_at AT TIME ZONE $1) AT TIME ZONE $1), -1,
  COUNT(*), COUNT(*) FILTER (WHERE d.prize_kind <> 'none'), COALESCE(SUM(`+lotteryIssuedPointsSQL+`), 0)
FROM lottery_draws d
WHERE EXISTS (SELECT 1 FROM lottery_activities a WHERE a.id = d.activity_id)
GROUP BY 1, 2
ON CONFLICT (activity_id, day_start, shard) DO NOTHING`, timezone); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO lottery_risk_keys (activity_id, day_start, kind, key, user_id)
SELECT DISTINCT d.activity_id, $1::timestamptz, k.kind, k.key, d.user_id
FROM lottery_draws d
CROSS JOIN LATERAL (VALUES
  ($2::smallint, d.device_id),
  ($3::smallint, d.client_ip),
  ($4::smallint, CASE WHEN d.client_ip IS NOT NULL AND pg_input_is_valid(d.client_ip, 'inet')
     THEN network(set_masklen(d.client_ip::inet, CASE WHEN family(d.client_ip::inet) = 4 THEN 24 ELSE 64 END))::text END)
) AS k(kind, key)
WHERE d.created_at >= $1 AND k.key IS NOT NULL AND k.key <> ''
ON CONFLICT DO NOTHING`, dayStart, lotteryRiskKeyDevice, lotteryRiskKeyIP, lotteryRiskKeySubnet); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_backfills (name) VALUES ($1) ON CONFLICT DO NOTHING`,
		lotteryCountersBackfill); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
