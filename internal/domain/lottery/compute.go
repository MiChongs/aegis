package lottery

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"time"

	"aegis/pkg/montecarlo"
)

// 大规模推演：把「整个应用的人、按各自的身份、连续抽上若干天」完整地演一遍，而且演很多遍。
//
// 策略模拟（simulate.go）回答的是「一批同样的人各抽 N 次会怎样」：一种画像、一天、一遍，
// 结果只有一个数，不知道它有多大的偶然成分。运营真正要问的是另一类问题：
//
//   - 限量的月度会员按现在的概率几天会被抽空？每日限量在一天的什么时候用完？
//   - 普通用户、会员、永久会员混在一起抽，积分预算够不够、谁拿走了多少？
//   - 永久会员走运那天用积分买次数到底赚不赚？不是按公示概率估，而是把软保底、中奖加成、
//     每日运气、个人限额、全站反馈全部演进去之后的真实回报。
//   - 上面这些数字各自有多大把握？
//
// ── 怎么演 ──
//
// 一个「平行世界」= Users 个人（按占比分成几种身份）× Days 天 × 每天若干次。同一个世界里的人共享库存、
// 全站每日限量、积分预算与全站计数；每天按随机顺序轮流抽（一轮一轮地排，不是一个人抽完再换下一个），
// 时间随之从零点走到二十四点，于是匀速释放、按时间进度放量的预算、规则里的 draw.hour 都按真实的节奏生效。
// 过了零点，每日限量与当日计数清零、每个人换一个运气系数，连败、累计中奖、个人上限接着往下走。
// 每一次抽奖走的是线上同一套 PlanDraw / PityChoice。
//
// 同样的世界独立演 Replications 遍（pkg/montecarlo：各自一条随机流、并行执行、按序号合并），
// 世界之间的差异就是统计误差：每个指标给出均值与 95% 置信区间（Student t）；
// 稀有奖品的出现比例另给 Wilson 区间。同一个种子在几个核上跑都得到同一份结果。

// 规模上界。
const (
	MaxComputeUsers        = 100_000
	MaxComputeDays         = 60
	MaxComputeDrawsPerDay  = 200
	MaxComputeReplications = 256
	MaxComputePersonas     = 6
	// MaxComputeDraws 一次推演的抽奖总次数上限
	MaxComputeDraws = 30_000_000
	// computeLevel 置信水平
	computeLevel = 0.95
	// computeBuckets 直方图最多分几段
	computeBuckets = 40
)

// ComputePersona 一种身份的人：占比、他们眼里的活动与奖盘、行为画像。
type ComputePersona struct {
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Share float64 `json:"share"`
	// Activity 这类人眼里的活动（人群特权改写后的保底次数等），策略在 Activity.Strategy
	Activity Activity `json:"activity"`
	// Prizes 个人奖盘（已拥有剔除、中奖加成之后，乘运气之前）。各身份的奖盘与基础奖盘逐格对应
	Prizes  []Prize         `json:"prizes"`
	Profile BehaviorProfile `json:"profile"`
	// Supreme 这类人能得到的至尊加成参数组
	Supreme []SupremeGrade `json:"supreme,omitempty"`
	// Payout / Redraws 抽中第 i 格时实际到账的积分与抽奖次数（含已拥有的折算）；为空时按奖品本身
	Payout  []float64 `json:"payout,omitempty"`
	Redraws []float64 `json:"redraws,omitempty"`
	// DrawsPerDay 这类人每天抽几次；0 为跟随场景
	DrawsPerDay int `json:"drawsPerDay,omitempty"`
}

// ComputeScenario 一次推演的全部输入（可序列化：任务落库后在任何实例上都能原样重跑）。
type ComputeScenario struct {
	Personas     []ComputePersona `json:"personas"`
	Users        int              `json:"users"`
	Days         int              `json:"days"`
	DrawsPerDay  int              `json:"drawsPerDay"`
	Replications int              `json:"replications"`
	Seed         uint64           `json:"seed"`
	Source       string           `json:"source,omitempty"`
	// Start 第一天的零点（应用时区）
	Start time.Time `json:"start"`
	// Price 用积分换一次抽奖的最低价（算是否稳赚用）；0 表示积分换不到次数
	Price int64 `json:"price"`
	// Workers 同时演几个世界；0 为自动
	Workers int `json:"-"`
}

// TotalDraws 这个场景一共要抽多少次。
func (s ComputeScenario) TotalDraws() int64 {
	var perWorld float64
	shares := normalizedShares(s.Personas)
	for index, persona := range s.Personas {
		draws := persona.DrawsPerDay
		if draws <= 0 {
			draws = s.DrawsPerDay
		}
		perWorld += shares[index] * float64(s.Users) * float64(draws)
	}
	return int64(math.Round(perWorld)) * int64(s.Days) * int64(s.Replications)
}

func normalizedShares(personas []ComputePersona) []float64 {
	shares := make([]float64, len(personas))
	var sum float64
	for index, persona := range personas {
		shares[index] = math.Max(persona.Share, 0)
		sum += shares[index]
	}
	for index := range shares {
		if sum > 0 {
			shares[index] /= sum
		} else {
			shares[index] = 1 / float64(len(shares))
		}
	}
	return shares
}

// ValidateCompute 校验推演规模。
func ValidateCompute(s ComputeScenario) error {
	switch {
	case len(s.Personas) == 0 || len(s.Personas) > MaxComputePersonas:
		return fmt.Errorf("人群需要 1–%d 种", MaxComputePersonas)
	case s.Users < 1 || s.Users > MaxComputeUsers:
		return fmt.Errorf("每个世界的人数需要在 1–%d 之间", MaxComputeUsers)
	case s.Days < 1 || s.Days > MaxComputeDays:
		return fmt.Errorf("天数需要在 1–%d 之间", MaxComputeDays)
	case s.DrawsPerDay < 1 || s.DrawsPerDay > MaxComputeDrawsPerDay:
		return fmt.Errorf("每人每天的抽奖次数需要在 1–%d 之间", MaxComputeDrawsPerDay)
	case s.Replications < 1 || s.Replications > MaxComputeReplications:
		return fmt.Errorf("平行世界数需要在 1–%d 之间", MaxComputeReplications)
	case s.Source != "" && !slices.Contains(Sources(), s.Source):
		return fmt.Errorf("未知的次数来源：%s", s.Source)
	}
	count := len(s.Personas[0].Prizes)
	if count == 0 {
		return fmt.Errorf("奖盘是空的")
	}
	var share float64
	for _, persona := range s.Personas {
		switch {
		case len(persona.Prizes) != count:
			return fmt.Errorf("人群「%s」的奖盘与基础奖盘对不上", persona.Name)
		case persona.Share < 0 || math.IsNaN(persona.Share):
			return fmt.Errorf("人群「%s」的占比不能为负", persona.Name)
		case persona.DrawsPerDay < 0 || persona.DrawsPerDay > MaxComputeDrawsPerDay:
			return fmt.Errorf("人群「%s」每天的抽奖次数需要在 0–%d 之间", persona.Name, MaxComputeDrawsPerDay)
		case persona.Payout != nil && len(persona.Payout) != count, persona.Redraws != nil && len(persona.Redraws) != count:
			return fmt.Errorf("人群「%s」的到账表与奖盘对不上", persona.Name)
		}
		share += persona.Share
	}
	if share <= 0 {
		return fmt.Errorf("人群占比合计需要大于 0")
	}
	if total := s.TotalDraws(); total > MaxComputeDraws {
		return fmt.Errorf("总抽奖次数 %d 超过上限 %d：请减少人数、天数、次数或平行世界数", total, MaxComputeDraws)
	}
	return nil
}

// ── 结果 ──

// ComputeDistribution 整数量（次数）的分布。
type ComputeDistribution struct {
	Mean    float64             `json:"mean"`
	P50     int                 `json:"p50"`
	P90     int                 `json:"p90"`
	P99     int                 `json:"p99"`
	Max     int                 `json:"max"`
	Buckets []montecarlo.Bucket `json:"buckets"`
}

// ComputeQuantiles 连续量的分布摘要。
type ComputeQuantiles struct {
	Mean float64 `json:"mean"`
	P10  float64 `json:"p10"`
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
}

// ComputeStockOut 限量奖品被抽空的情况。
type ComputeStockOut struct {
	// Probability 有多大比例的世界在推演期内抽空了它
	Probability float64 `json:"probability"`
	// Day 抽空发生在第几天（从 1 起，带小数：1.5 = 第一天正午）；只统计抽空了的世界
	Day montecarlo.Estimate `json:"day"`
}

// ComputeDailyCap 全站每日限量的触顶情况。
type ComputeDailyCap struct {
	// Share 有多大比例的日子触顶
	Share float64 `json:"share"`
	// At 触顶发生在一天的什么时候（0–1，0.5 = 正午）；只统计触顶的日子
	At float64 `json:"at"`
}

// ComputePrize 一个奖品的推演结果。
type ComputePrize struct {
	PrizeID  int64  `json:"prizeId"`
	Position int    `json:"position"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Wins     int64  `json:"wins"`
	// Rate 出现比例（各世界的均值与区间）；Low / High 全部世界合在一起的 Wilson 区间（稀有奖品看这个）
	Rate montecarlo.Estimate `json:"rate"`
	Low  float64             `json:"low"`
	High float64             `json:"high"`
	// BaseRate 公示概率（按人群占比加权）
	BaseRate float64 `json:"baseRate"`
	// Blocked 有多少次这个奖品出不了：被硬性资格挡掉，或被预算、匀速释放、规则清零
	Blocked  int64            `json:"blocked"`
	StockOut *ComputeStockOut `json:"stockOut,omitempty"`
	DailyCap *ComputeDailyCap `json:"dailyCap,omitempty"`
}

// ComputePersonaResult 一种身份的人的结果。
type ComputePersonaResult struct {
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Share float64 `json:"share"`
	Users int     `json:"users"`
	// Draws 这类人在全部世界里合计抽了多少次
	Draws         int64               `json:"draws"`
	WinRate       montecarlo.Estimate `json:"winRate"`
	RealRate      montecarlo.Estimate `json:"realRate"`
	ValuePerDraw  montecarlo.Estimate `json:"valuePerDraw"`
	PointsPerDraw montecarlo.Estimate `json:"pointsPerDraw"`
	// Return 每抽期望积分回报 P / (1 − Q)（再得的次数接着抽）
	Return montecarlo.Estimate `json:"return"`
	// Arbitrage 这类人用积分买次数稳赚（回报的置信区间下界超过价格）
	Arbitrage bool `json:"arbitrage,omitempty"`
	// Prizes 各奖品在这类人身上的出现比例（与 ComputeResult.Prizes 同序）
	Prizes []float64 `json:"prizes"`
}

// ComputeDay 一天的平均情况。
type ComputeDay struct {
	Day      int     `json:"day"`
	Draws    float64 `json:"draws"`
	WinRate  float64 `json:"winRate"`
	RealRate float64 `json:"realRate"`
	// Value / Points 这一天发出去的价值与积分（每个世界的平均）
	Value  float64 `json:"value"`
	Points float64 `json:"points"`
}

// ComputeStockSeries 一个限量奖品的库存消耗曲线。
type ComputeStockSeries struct {
	PrizeID  int64  `json:"prizeId"`
	Position int    `json:"position"`
	Name     string `json:"name"`
	Quantity int64  `json:"quantity"`
	// Used 每天结束时累计已出的数量（各世界的平均），含推演之前已出的
	Used []float64 `json:"used"`
}

// ComputeBudget 每日积分预算的触顶情况。
type ComputeBudget struct {
	DailyPoints int64 `json:"dailyPoints"`
	// Share 有多大比例的日子把预算用完；At 用完时是一天的什么时候（0–1）
	Share float64 `json:"share"`
	At    float64 `json:"at"`
	// Used 每天平均用掉预算的几成
	Used float64 `json:"used"`
}

// ComputeArbitrage 「用积分买次数、抽完再买」在推演里到底赚不赚。
type ComputeArbitrage struct {
	Price int64 `json:"price"`
	// Return 全体用户每抽期望积分回报 P / (1 − Q)
	Return montecarlo.Estimate `json:"return"`
	// Probability 有多大比例的世界里回报超过价格
	Probability float64 `json:"probability"`
	// Exceeds 回报的置信区间下界超过价格：稳赚
	Exceeds bool `json:"exceeds"`
}

// ComputePrecision 这次推演的精度。
type ComputePrecision struct {
	// ValueError / RateError 每抽价值与真实奖品率的置信区间半宽相对均值的比例
	ValueError float64 `json:"valueError"`
	RateError  float64 `json:"rateError"`
	// Enough 两项都在 2% 以内
	Enough bool `json:"enough"`
}

// ComputeResult 推演结果。
type ComputeResult struct {
	Users        int   `json:"users"`
	Days         int   `json:"days"`
	DrawsPerDay  int   `json:"drawsPerDay"`
	Replications int   `json:"replications"`
	Draws        int64 `json:"draws"`
	// Seed 种子（十进制字符串：64 位整数超出了 JSON 数字的安全范围）
	Seed string `json:"seed"`

	WinRate        montecarlo.Estimate `json:"winRate"`
	RealRate       montecarlo.Estimate `json:"realRate"`
	ValuePerDraw   montecarlo.Estimate `json:"valuePerDraw"`
	PointsPerDraw  montecarlo.Estimate `json:"pointsPerDraw"`
	RedrawsPerDraw montecarlo.Estimate `json:"redrawsPerDraw"`
	// PityShare 硬保底给出的抽奖占比；SoftPityShare 软保底生效的占比；FallbackShare 所有权重为 0、改发兜底的占比
	PityShare     montecarlo.Estimate `json:"pityShare"`
	SoftPityShare montecarlo.Estimate `json:"softPityShare"`
	FallbackShare montecarlo.Estimate `json:"fallbackShare"`
	// PointsPerWorld 一个世界在整个推演期里发出的积分
	PointsPerWorld montecarlo.Estimate `json:"pointsPerWorld"`
	// NeverWon 一次真实奖品都没抽到的人占多少
	NeverWon montecarlo.Estimate `json:"neverWon"`

	Prizes   []ComputePrize         `json:"prizes"`
	Personas []ComputePersonaResult `json:"personas"`
	Daily    []ComputeDay           `json:"daily"`
	Stock    []ComputeStockSeries   `json:"stock"`
	Budget   *ComputeBudget         `json:"budget,omitempty"`

	// WinsPerUser 每人抽到真实奖品的次数；MissStreak 每人最长连续没抽到真实奖品的次数；FirstWin 第几抽第一次抽到
	WinsPerUser ComputeDistribution `json:"winsPerUser"`
	MissStreak  ComputeDistribution `json:"missStreak"`
	FirstWin    ComputeDistribution `json:"firstWin"`
	// ValuePerUser / PointsPerUser 每人在整个推演期里拿到的价值与积分
	ValuePerUser  ComputeQuantiles  `json:"valuePerUser"`
	PointsPerUser ComputeQuantiles  `json:"pointsPerUser"`
	Fortune       *ComputeQuantiles `json:"fortune,omitempty"`

	Arbitrage *ComputeArbitrage `json:"arbitrage,omitempty"`
	Precision ComputePrecision  `json:"precision"`
}

// ── 一个世界 ──

type computeUser struct {
	persona                      uint8
	missStreak, total, wins      int32
	today, winsToday             int32
	realStreak, longestReal      int32
	realWins, firstReal          int32
	luckE, luckV, luckS, fortune float64
	value, points                float64
	prizeWins, prizeToday        map[int64]int
	recent                       []int64
}

type computePersonaStats struct {
	users                  int
	draws, wins, real      int64
	value, points, redraws float64
	prizeWins              []int64
}

type computeDayStats struct {
	draws, wins, real int64
	value, points     float64
	stockUsed         []int64
}

type computeWorld struct {
	draws, wins, real, pity, softPity, empty int64
	value, points, redraws                   float64
	prizeWins, prizeBlocked                  []int64
	// stockOutAt 抽空的时刻（天 + 当天进度，从 1 起）；没抽空为 0
	stockOutAt []float64
	// capDays / capAt 每日限量触顶的天数与触顶时刻之和
	capDays []int
	capAt   []float64
	// budgetDays / budgetAt / budgetUsed 积分预算用完的天数、用完时刻之和、每天用掉的比例之和
	budgetDays           int
	budgetAt, budgetUsed float64
	personas             []computePersonaStats
	days                 []computeDayStats
	winsPerUser          montecarlo.Histogram
	missStreak, firstWin montecarlo.Histogram
	neverWon             int64
	valuePerUser         *montecarlo.Sketch
	pointsPerUser        *montecarlo.Sketch
	fortune              *montecarlo.Sketch
}

// personaLayout 各身份的人数（最大余数法，合计恰为 users）。
func personaLayout(personas []ComputePersona, users int) []int {
	shares := normalizedShares(personas)
	counts := make([]int, len(personas))
	type remainder struct {
		index int
		frac  float64
	}
	rest := make([]remainder, len(personas))
	assigned := 0
	for index, share := range shares {
		exact := share * float64(users)
		counts[index] = int(math.Floor(exact))
		rest[index] = remainder{index, exact - math.Floor(exact)}
		assigned += counts[index]
	}
	slices.SortStableFunc(rest, func(a, b remainder) int {
		switch {
		case a.frac > b.frac:
			return -1
		case a.frac < b.frac:
			return 1
		}
		return 0
	})
	for i := 0; assigned < users; i++ {
		counts[rest[i%len(rest)].index]++
		assigned++
	}
	return counts
}

// fortuneInto 把运气系数乘到 dst 上（与 FortunePrizes 同一个算法，不分配内存）。
func fortuneInto(dst, src []Prize, factor float64) {
	copy(dst, src)
	if factor <= 0 || factor == 1 {
		return
	}
	for index := range dst {
		weight := dst[index].Weight * fortuneScale
		if dst[index].Winning() && !dst[index].Fallback {
			weight = int(math.Round(float64(weight) * factor))
		}
		dst[index].Weight = weight
	}
}

// runComputeWorld 演一个世界。
func runComputeWorld(ctx context.Context, scenario ComputeScenario, rng *rand.Rand) (*computeWorld, error) {
	personas := scenario.Personas
	count := len(personas[0].Prizes)
	source := scenario.Source
	if source == "" {
		source = SourceFree
	}
	world := &computeWorld{
		prizeWins: make([]int64, count), prizeBlocked: make([]int64, count), stockOutAt: make([]float64, count),
		capDays: make([]int, count), capAt: make([]float64, count),
		personas:     make([]computePersonaStats, len(personas)),
		days:         make([]computeDayStats, scenario.Days),
		valuePerUser: montecarlo.NewSketch(), pointsPerUser: montecarlo.NewSketch(),
	}

	// 每种身份一份奖盘（库存逐格同步）、估值、到账表、每天的次数
	boards := make([][]Prize, len(personas))
	values := make([][]float64, len(personas))
	daily := make([]int, len(personas))
	fallbackAt := make([]int, len(personas))
	rounds := 0
	for p, persona := range personas {
		boards[p] = slices.Clone(persona.Prizes)
		values[p] = make([]float64, count)
		for i, prize := range persona.Prizes {
			values[p][i] = PrizeValue(persona.Activity, persona.Activity.Strategy.Guard, prize)
		}
		daily[p] = persona.DrawsPerDay
		if daily[p] <= 0 {
			daily[p] = scenario.DrawsPerDay
		}
		rounds = max(rounds, daily[p])
		fallbackAt[p] = -1
		if fallback, ok := FallbackPrize(persona.Prizes); ok {
			fallbackAt[p] = slices.IndexFunc(persona.Prizes, func(item Prize) bool { return item.ID == fallback.ID })
		}
		world.personas[p].prizeWins = make([]int64, count)
	}
	strategy := personas[0].Activity.Strategy
	cooldown := 0
	if strategy.Cooldown != nil {
		cooldown = strategy.Cooldown.Draws
	}
	fortuneOn := strategy.Fortune != nil && strategy.Fortune.Spread > 0
	if fortuneOn {
		world.fortune = montecarlo.NewSketch()
	}
	// 积分预算「用完」：剩下的预算连最小的一个积分奖都发不出（策略在单个奖品超过剩余预算时不出，
	// 所以已发的数很少正好等于预算）
	var budget, smallestPoints int64
	if strategy.Budget != nil {
		budget = strategy.Budget.DailyPoints
		for _, prize := range personas[0].Prizes {
			if amount := prize.Reward.Amount; prize.Kind == KindPoints && amount > 0 && (smallestPoints == 0 || amount < smallestPoints) {
				smallestPoints = amount
			}
		}
		smallestPoints = max(smallestPoints, 1)
	}

	layout := personaLayout(personas, scenario.Users)
	users := make([]computeUser, 0, scenario.Users)
	for p, n := range layout {
		world.personas[p].users = n
		for range n {
			users = append(users, computeUser{persona: uint8(p), fortune: 1})
		}
	}
	order := make([]int, len(users))
	for index := range order {
		order[index] = index
	}
	scratch := make([]Prize, count)
	dailyUsed := map[int64]int64{}
	start := scenario.Start
	if start.IsZero() {
		start = time.Now()
	}

	for day := range scenario.Days {
		clear(dailyUsed)
		var pointsToday int64
		activityDraws, activityWins := 0, 0
		budgetHit := false
		capHit := make([]bool, count)
		dayStart := start.AddDate(0, 0, day)
		stats := &world.days[day]
		for index := range users {
			user := &users[index]
			user.today, user.winsToday, user.prizeToday = 0, 0, nil
			if fortuneOn {
				natural := fortuneFactor(strategy.Fortune.Spread, rng.Uint64())
				if grades := personas[user.persona].Supreme; len(grades) > 0 {
					_, natural = BestSupreme(grades, natural)
				}
				user.fortune = natural
				world.fortune.Add(natural)
			}
		}
		rng.Shuffle(len(order), func(a, b int) { order[a], order[b] = order[b], order[a] })

		for round := range rounds {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for slot, userIndex := range order {
				user := &users[userIndex]
				p := int(user.persona)
				if round >= daily[p] {
					continue
				}
				persona := &personas[p]
				progress := (float64(round) + (float64(slot)+0.5)/float64(len(order))) / float64(rounds)
				now := dayStart.Add(time.Duration(progress * float64(24*time.Hour)))
				state := DrawState{
					Now: now, Source: source, MissStreak: int(user.missStreak), TotalDraws: int(user.total),
					TodayDraws: int(user.today), WinsTotal: int(user.wins), WinsToday: int(user.winsToday),
					PrizeWins: user.prizeWins, PrizeWinsToday: user.prizeToday, Recent: user.recent,
					PrizeDailyUsed: dailyUsed, PointsIssuedToday: pointsToday,
					DeviceAccounts: 1, IPAccounts: 1, SubnetAccounts: 1, DeviceHistory: 1,
					RecentDraws: int(user.today), Profile: persona.Profile, DayProgress: progress,
					ActivityDrawsToday: activityDraws, ActivityWinsToday: activityWins,
					LuckExpected: user.luckE, LuckVariance: user.luckV, LuckSkew: user.luckS,
				}
				fortuneInto(scratch, boards[p], user.fortune)
				plan := PlanDraw(persona.Activity, strategy, scratch, state)
				if plan.RiskReport.Denied() {
					continue
				}
				if plan.SoftPity > 0 {
					world.softPity++
				}
				// 这一次出不了的奖品：被硬性资格挡掉，或被某个维度（预算用完、匀速释放暂停、规则）清零
				for index, odds := range plan.Odds {
					if odds.Weight <= 0 && boards[p][index].Weight > 0 {
						world.prizeBlocked[index]++
					}
				}
				index, pity := PityChoice(persona.Activity, plan, scratch, int(user.missStreak))
				if pity {
					world.pity++
				} else {
					var total float64
					for _, odds := range plan.Odds {
						total += odds.Weight
					}
					if total > 0 {
						// 逆累积分布抽样：一个均匀数落在哪一格
						roll := rng.Float64() * total
						index = -1
						last := 0
						for i, odds := range plan.Odds {
							if odds.Weight <= 0 {
								continue
							}
							last = i
							if roll < odds.Weight {
								index = i
								break
							}
							roll -= odds.Weight
						}
						if index < 0 {
							index = last // 浮点累加的尾差
						}
					} else {
						world.empty++
						if index = fallbackAt[p]; index < 0 {
							continue
						}
					}
				}
				prize := &boards[p][index]
				winning, isReal := prize.Winning(), prize.Winning() && !prize.Fallback
				if prize.Quantity >= 0 {
					used := prize.Used + 1
					for q := range boards {
						boards[q][index].Used = used
					}
					if used >= prize.Quantity && world.stockOutAt[index] == 0 {
						world.stockOutAt[index] = float64(day) + 1 + progress
					}
				}
				if prize.DailyLimit > 0 {
					dailyUsed[prize.ID]++
					if !capHit[index] && dailyUsed[prize.ID] >= int64(prize.DailyLimit) {
						capHit[index] = true
						world.capDays[index]++
						world.capAt[index] += progress
					}
				}
				payout, redraws := 0.0, 0.0
				switch {
				case persona.Payout != nil:
					payout = persona.Payout[index]
				case prize.Kind == KindPoints:
					payout = float64(prize.Reward.Amount)
				}
				switch {
				case persona.Redraws != nil:
					redraws = persona.Redraws[index]
				case prize.Kind == KindLotteryDraws:
					redraws = float64(prize.Reward.Amount)
				}
				value := values[p][index]

				pointsToday += int64(payout)
				if budget > 0 && !budgetHit && budget-pointsToday < smallestPoints {
					budgetHit = true
					world.budgetDays++
					world.budgetAt += progress
				}
				activityDraws++
				world.draws++
				world.prizeWins[index]++
				world.value += value
				world.points += payout
				world.redraws += redraws
				stats.draws++
				stats.value += value
				stats.points += payout
				group := &world.personas[p]
				group.draws++
				group.prizeWins[index]++
				group.value += value
				group.points += payout
				group.redraws += redraws

				user.total++
				user.today++
				user.value += value
				user.points += payout
				user.luckE += plan.Fair
				user.luckV += plan.Fair * (1 - plan.Fair)
				user.luckS += plan.Fair * (1 - plan.Fair) * (1 - 2*plan.Fair)
				if winning {
					activityWins++
					world.wins++
					stats.wins++
					group.wins++
					user.wins++
					user.winsToday++
					if user.prizeWins == nil {
						user.prizeWins = map[int64]int{}
					}
					user.prizeWins[prize.ID]++
					if user.prizeToday == nil {
						user.prizeToday = map[int64]int{}
					}
					user.prizeToday[prize.ID]++
				}
				if isReal {
					world.real++
					stats.real++
					group.real++
					user.realWins++
					if user.firstReal == 0 {
						user.firstReal = user.total
					}
					user.realStreak = 0
				} else {
					user.realStreak++
					user.longestReal = max(user.longestReal, user.realStreak)
				}
				user.missStreak = int32(NextMissStreak(int(user.missStreak), *prize))
				if cooldown > 0 {
					id := int64(0)
					if winning {
						id = prize.ID
					}
					if len(user.recent) < cooldown {
						user.recent = append(user.recent, 0)
					}
					copy(user.recent[1:], user.recent)
					user.recent[0] = id
				}
			}
		}
		if budget > 0 {
			world.budgetUsed += math.Min(1, float64(pointsToday)/float64(budget))
		}
		stats.stockUsed = make([]int64, count)
		for index := range boards[0] {
			stats.stockUsed[index] = boards[0][index].Used
		}
	}

	for index := range users {
		user := &users[index]
		world.winsPerUser.Add(int(user.realWins))
		world.missStreak.Add(int(user.longestReal))
		if user.firstReal > 0 {
			world.firstWin.Add(int(user.firstReal))
		} else if user.total > 0 {
			world.neverWon++
		}
		world.valuePerUser.Add(user.value)
		world.pointsPerUser.Add(user.points)
	}
	return world, nil
}

// ── 合并 ──

type computePersonaAcc struct {
	draws                                  int64
	winRate, realRate, value, points, back montecarlo.Moments
	prizeWins                              []int64
}

type computeAcc struct {
	scenario                                    ComputeScenario
	draws                                       int64
	winRate, realRate, value, points, redraws   montecarlo.Moments
	pity, softPity, empty, pointsWorld, never   montecarlo.Moments
	back                                        montecarlo.Moments
	backOver                                    int
	prizeWins, prizeBlocked                     []int64
	prizeRate                                   []montecarlo.Moments
	stockOut                                    []montecarlo.Moments
	capDays                                     []int
	capAt                                       []float64
	budgetDays                                  int
	budgetAt, budgetUsed                        float64
	personas                                    []computePersonaAcc
	dayDraws, dayWins, dayReal, dayValue, dayPt []float64
	stockUsed                                   [][]float64
	winsPerUser, missStreak, firstWin           montecarlo.Histogram
	valuePerUser, pointsPerUser, fortune        *montecarlo.Sketch
	worlds                                      int
}

func newComputeAcc(scenario ComputeScenario) *computeAcc {
	count := len(scenario.Personas[0].Prizes)
	acc := &computeAcc{
		scenario:  scenario,
		prizeWins: make([]int64, count), prizeBlocked: make([]int64, count),
		prizeRate: make([]montecarlo.Moments, count), stockOut: make([]montecarlo.Moments, count),
		capDays: make([]int, count), capAt: make([]float64, count),
		personas: make([]computePersonaAcc, len(scenario.Personas)),
		dayDraws: make([]float64, scenario.Days), dayWins: make([]float64, scenario.Days),
		dayReal: make([]float64, scenario.Days), dayValue: make([]float64, scenario.Days), dayPt: make([]float64, scenario.Days),
		stockUsed:    make([][]float64, count),
		valuePerUser: montecarlo.NewSketch(), pointsPerUser: montecarlo.NewSketch(),
	}
	for index := range acc.stockUsed {
		acc.stockUsed[index] = make([]float64, scenario.Days)
	}
	for index := range acc.personas {
		acc.personas[index].prizeWins = make([]int64, count)
	}
	return acc
}

// pointsReturn 每抽期望积分回报 P / (1 − Q)；抽不完时为 +Inf。
func pointsReturn(points, redraws float64, draws int64) float64 {
	if draws <= 0 {
		return 0
	}
	p, q := points/float64(draws), redraws/float64(draws)
	if q >= 1 {
		return math.Inf(1)
	}
	return p / (1 - q)
}

func (acc *computeAcc) add(world *computeWorld) {
	acc.worlds++
	acc.draws += world.draws
	if world.draws > 0 {
		n := float64(world.draws)
		acc.winRate.Add(float64(world.wins) / n)
		acc.realRate.Add(float64(world.real) / n)
		acc.value.Add(world.value / n)
		acc.points.Add(world.points / n)
		acc.redraws.Add(world.redraws / n)
		acc.pity.Add(float64(world.pity) / n)
		acc.softPity.Add(float64(world.softPity) / n)
		acc.empty.Add(float64(world.empty) / n)
		back := pointsReturn(world.points, world.redraws, world.draws)
		if !math.IsInf(back, 0) {
			acc.back.Add(back)
		}
		if acc.scenario.Price > 0 && back > float64(acc.scenario.Price) {
			acc.backOver++
		}
		for index := range world.prizeWins {
			acc.prizeRate[index].Add(float64(world.prizeWins[index]) / n)
		}
	}
	acc.pointsWorld.Add(world.points)
	acc.never.Add(float64(world.neverWon) / float64(max(acc.scenario.Users, 1)))
	for index := range world.prizeWins {
		acc.prizeWins[index] += world.prizeWins[index]
		acc.prizeBlocked[index] += world.prizeBlocked[index]
		if world.stockOutAt[index] > 0 {
			acc.stockOut[index].Add(world.stockOutAt[index])
		}
		acc.capDays[index] += world.capDays[index]
		acc.capAt[index] += world.capAt[index]
	}
	acc.budgetDays += world.budgetDays
	acc.budgetAt += world.budgetAt
	acc.budgetUsed += world.budgetUsed
	for p, group := range world.personas {
		target := &acc.personas[p]
		target.draws += group.draws
		if group.draws > 0 {
			n := float64(group.draws)
			target.winRate.Add(float64(group.wins) / n)
			target.realRate.Add(float64(group.real) / n)
			target.value.Add(group.value / n)
			target.points.Add(group.points / n)
			if back := pointsReturn(group.points, group.redraws, group.draws); !math.IsInf(back, 0) {
				target.back.Add(back)
			}
		}
		for index, wins := range group.prizeWins {
			target.prizeWins[index] += wins
		}
	}
	for day, stats := range world.days {
		acc.dayDraws[day] += float64(stats.draws)
		acc.dayWins[day] += float64(stats.wins)
		acc.dayReal[day] += float64(stats.real)
		acc.dayValue[day] += stats.value
		acc.dayPt[day] += stats.points
		for index, used := range stats.stockUsed {
			acc.stockUsed[index][day] += float64(used)
		}
	}
	acc.winsPerUser.Merge(&world.winsPerUser)
	acc.missStreak.Merge(&world.missStreak)
	acc.firstWin.Merge(&world.firstWin)
	acc.valuePerUser.Merge(world.valuePerUser)
	acc.pointsPerUser.Merge(world.pointsPerUser)
	if world.fortune != nil {
		if acc.fortune == nil {
			acc.fortune = montecarlo.NewSketch()
		}
		acc.fortune.Merge(world.fortune)
	}
}

func distributionOf(h *montecarlo.Histogram) ComputeDistribution {
	return ComputeDistribution{
		Mean: round6(h.Mean()), P50: h.Quantile(0.5), P90: h.Quantile(0.9), P99: h.Quantile(0.99),
		Max: h.Max(), Buckets: h.Buckets(computeBuckets),
	}
}

func quantilesOf(s *montecarlo.Sketch) ComputeQuantiles {
	return ComputeQuantiles{
		Mean: round6(s.Mean()), P10: round6(s.Quantile(0.1)), P50: round6(s.Quantile(0.5)),
		P90: round6(s.Quantile(0.9)), P99: round6(s.Quantile(0.99)), Max: round6(s.Max()),
	}
}

func estimateOf(m montecarlo.Moments) montecarlo.Estimate {
	e := m.Estimate(computeLevel)
	e.Mean, e.StdErr, e.Low, e.High = round6(e.Mean), round6(e.StdErr), round6(e.Low), round6(e.High)
	return e
}

func (acc *computeAcc) result() ComputeResult {
	scenario := acc.scenario
	worlds := float64(max(acc.worlds, 1))
	shares := normalizedShares(scenario.Personas)
	layout := personaLayout(scenario.Personas, scenario.Users)
	out := ComputeResult{
		Users: scenario.Users, Days: scenario.Days, DrawsPerDay: scenario.DrawsPerDay, Replications: acc.worlds,
		Draws: acc.draws, Seed: strconv.FormatUint(scenario.Seed, 10),
		WinRate: estimateOf(acc.winRate), RealRate: estimateOf(acc.realRate), ValuePerDraw: estimateOf(acc.value),
		PointsPerDraw: estimateOf(acc.points), RedrawsPerDraw: estimateOf(acc.redraws),
		PityShare: estimateOf(acc.pity), SoftPityShare: estimateOf(acc.softPity), FallbackShare: estimateOf(acc.empty),
		PointsPerWorld: estimateOf(acc.pointsWorld), NeverWon: estimateOf(acc.never),
		Prizes: make([]ComputePrize, len(acc.prizeWins)), Personas: make([]ComputePersonaResult, len(scenario.Personas)),
		Daily: make([]ComputeDay, scenario.Days), Stock: []ComputeStockSeries{},
		WinsPerUser: distributionOf(&acc.winsPerUser), MissStreak: distributionOf(&acc.missStreak),
		FirstWin:     distributionOf(&acc.firstWin),
		ValuePerUser: quantilesOf(acc.valuePerUser), PointsPerUser: quantilesOf(acc.pointsPerUser),
	}
	if acc.fortune != nil {
		fortune := quantilesOf(acc.fortune)
		out.Fortune = &fortune
	}
	base := scenario.Personas[0].Prizes
	nominal := make([][]float64, len(scenario.Personas))
	for p, persona := range scenario.Personas {
		nominal[p] = Probabilities(persona.Prizes)
	}
	totalDays := float64(max(acc.worlds*scenario.Days, 1))
	for index, prize := range base {
		item := ComputePrize{
			PrizeID: prize.ID, Position: prize.Position, Name: prize.Name, Kind: prize.Kind,
			Wins: acc.prizeWins[index], Rate: estimateOf(acc.prizeRate[index]), Blocked: acc.prizeBlocked[index],
		}
		item.Low, item.High = montecarlo.Wilson(acc.prizeWins[index], acc.draws, computeLevel)
		for p := range scenario.Personas {
			item.BaseRate += shares[p] * nominal[p][index] / 100
		}
		item.BaseRate = round6(item.BaseRate)
		if prize.Quantity >= 0 {
			item.StockOut = &ComputeStockOut{
				Probability: round6(float64(acc.stockOut[index].N) / worlds), Day: estimateOf(acc.stockOut[index]),
			}
			series := ComputeStockSeries{PrizeID: prize.ID, Position: prize.Position, Name: prize.Name,
				Quantity: prize.Quantity, Used: make([]float64, scenario.Days)}
			for day := range series.Used {
				series.Used[day] = math.Round(acc.stockUsed[index][day]/worlds*100) / 100
			}
			out.Stock = append(out.Stock, series)
		}
		if prize.DailyLimit > 0 {
			dailyCap := &ComputeDailyCap{Share: round6(float64(acc.capDays[index]) / totalDays)}
			if acc.capDays[index] > 0 {
				dailyCap.At = round6(acc.capAt[index] / float64(acc.capDays[index]))
			}
			item.DailyCap = dailyCap
		}
		out.Prizes[index] = item
	}
	for p, persona := range scenario.Personas {
		group := acc.personas[p]
		item := ComputePersonaResult{
			Key: persona.Key, Name: persona.Name, Share: round6(shares[p]), Users: layout[p], Draws: group.draws,
			WinRate: estimateOf(group.winRate), RealRate: estimateOf(group.realRate),
			ValuePerDraw: estimateOf(group.value), PointsPerDraw: estimateOf(group.points), Return: estimateOf(group.back),
			Prizes: make([]float64, len(group.prizeWins)),
		}
		for index, wins := range group.prizeWins {
			if group.draws > 0 {
				item.Prizes[index] = round6(float64(wins) / float64(group.draws))
			}
		}
		// 只有一个世界时给不出区间，按均值判
		item.Arbitrage = scenario.Price > 0 && group.back.N > 0 && item.Return.Low > float64(scenario.Price)
		out.Personas[p] = item
	}
	for day := range out.Daily {
		item := ComputeDay{Day: day + 1, Draws: math.Round(acc.dayDraws[day]/worlds*100) / 100,
			Value: math.Round(acc.dayValue[day]/worlds*100) / 100, Points: math.Round(acc.dayPt[day]/worlds*100) / 100}
		if acc.dayDraws[day] > 0 {
			item.WinRate = round6(acc.dayWins[day] / acc.dayDraws[day])
			item.RealRate = round6(acc.dayReal[day] / acc.dayDraws[day])
		}
		out.Daily[day] = item
	}
	if strategy := scenario.Personas[0].Activity.Strategy; strategy.Budget != nil && strategy.Budget.DailyPoints > 0 {
		budget := &ComputeBudget{DailyPoints: strategy.Budget.DailyPoints,
			Share: round6(float64(acc.budgetDays) / totalDays), Used: round6(acc.budgetUsed / totalDays)}
		if acc.budgetDays > 0 {
			budget.At = round6(acc.budgetAt / float64(acc.budgetDays))
		}
		out.Budget = budget
	}
	if scenario.Price > 0 {
		back := estimateOf(acc.back)
		out.Arbitrage = &ComputeArbitrage{
			Price: scenario.Price, Return: back, Probability: round6(float64(acc.backOver) / worlds),
			Exceeds: acc.back.N > 0 && back.Low > float64(scenario.Price),
		}
	}
	out.Precision = ComputePrecision{
		ValueError: round6(out.ValuePerDraw.RelativeError()), RateError: round6(out.RealRate.RelativeError()),
	}
	out.Precision.Enough = acc.worlds >= 2 && out.Precision.ValueError <= 0.02 && out.Precision.RateError <= 0.02
	return out
}

// Compute 按场景推演。progress 每合并完一个世界调用一次（可为 nil）；ctx 取消时尽快停下并返回它的错误。
func Compute(ctx context.Context, scenario ComputeScenario, progress func(done, total int)) (ComputeResult, error) {
	if err := ValidateCompute(scenario); err != nil {
		return ComputeResult{}, err
	}
	acc := newComputeAcc(scenario)
	err := montecarlo.Run(ctx, montecarlo.Config{
		Replications: scenario.Replications, Workers: scenario.Workers, Seed: scenario.Seed, Progress: progress,
	}, func(ctx context.Context, _ int, rng *rand.Rand) (*computeWorld, error) {
		return runComputeWorld(ctx, scenario, rng)
	}, func(_ int, world *computeWorld) error {
		acc.add(world)
		return nil
	})
	if err != nil {
		return ComputeResult{}, err
	}
	return acc.result(), nil
}
