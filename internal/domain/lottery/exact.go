package lottery

import (
	"math"

	"gonum.org/v1/gonum/integrate/quad"
	"gonum.org/v1/gonum/stat/distuv"
)

// 精确分析：不抽样，直接算出一个奖盘「长期来看」每个奖品的综合概率、每一抽值多少、多少抽能出某个奖品。
//
// ── 模型 ──
//
// 把「已经连续未中 k 次」看成马尔可夫链的状态 k（与 markov.go 同一条链）。在状态 k 抽一次，按同一个
// PlanDraw / PityChoice 得到各奖品的概率 p_k(i)（硬保底到点时保底奖为 1）：抽中任何奖品回到状态 0，
// 没抽中进入 k + 1。软保底、伪随机分布、硬保底都只依赖 k，所以这条链把它们全部精确地算了进去。
//
// 这是一个更新过程：每次中奖开始一个新周期，周期长 T。由更新理论，链的平稳分布正比于生存函数
//
//	π_k = P(T > k) / E[T]
//
// 于是奖品 i 的**综合概率**（含保底，监管口径的「综合概率」）与每抽的期望价值是
//
//	L_i = Σ_k π_k · p_k(i),     价值 = Σ_i L_i · v_i
//
// ── 多少抽能出某个奖品 ──
//
// 对目标奖品 i，把「抽中 i」设为吸收态：在状态 k，以 a_k = p_k(i) 被吸收，以 r_k 回到 0（抽中别的奖品），
// 以 d_k 进入 k + 1（未中）。期望步数满足 t_k = 1 + r_k·t_0 + d_k·t_{k+1}，是关于 t_0 的线性递推：
// 设 t_k = α_k + β_k·t_0，从最后一个状态往回代，t_0 = α_0 / (1 − β_0)。O(状态数)，不用解稠密方程组
// （测试里与 gonum 的 LU 解 (I − Q)t = 1 逐位对照）。
// 分布（分位数、N 连抽至少出一个的概率）由状态向量逐步推进得到；推到衰减率稳定之后，
// 尾部按准平稳分布的几何衰减外推（万分之一的大奖不用真的推几十万步）。
//
// ── 没算进去的 ──
//
// 库存、全站每日限量、每人上限、中奖冷却、运气平衡依赖的不只是 k（它们随别人、随历史变化），
// 这里按分析时的状态固定处理，并在 Notes 里写明。要看它们的效果用大规模推演（compute.go）。

// ExactWithin N 连抽里至少出一个的概率。
type ExactWithin struct {
	Draws       int     `json:"draws"`
	Probability float64 `json:"probability"`
}

// ExactPrize 一个奖品的精确数字。
type ExactPrize struct {
	PrizeID  int64  `json:"prizeId"`
	Position int    `json:"position"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	// Nominal 公示概率（常规抽取，0–1）；LongRun 综合概率（含保底、软保底、伪随机，0–1）
	Nominal float64 `json:"nominal"`
	LongRun float64 `json:"longRun"`
	// Value 这个奖品的积分当量
	Value float64 `json:"value"`
	// HitMean 从零连败起，期望抽多少次出第一个；HitP50 / P90 / P99 分位数。Never 为真时出不了
	HitMean float64 `json:"hitMean"`
	HitP50  int     `json:"hitP50"`
	HitP90  int     `json:"hitP90"`
	HitP99  int     `json:"hitP99"`
	Never   bool    `json:"never,omitempty"`
	// Within 1 / 10 / 50 / 100 连抽里至少出一个的概率
	Within []ExactWithin `json:"within"`
}

// ExactAnalysis 一个奖盘（某个人眼里的）的精确分析。
type ExactAnalysis struct {
	// States 用了多少个连败状态（没有「谢谢参与」的奖盘只有 1 个）
	States int `json:"states"`
	// WinRate 综合中奖率；BaseWinRate 公示中奖率（常规抽取）
	WinRate     float64 `json:"winRate"`
	BaseWinRate float64 `json:"baseWinRate"`
	// RealRate 综合概率里抽中兜底奖、谢谢参与以外奖品的部分；BaseRealRate 公示口径
	RealRate     float64 `json:"realRate"`
	BaseRealRate float64 `json:"baseRealRate"`
	// ValuePerDraw 长期每抽的期望价值（积分当量）；PointsPerDraw 其中积分奖品的部分；
	// RedrawsPerDraw 每抽期望再得几次抽奖
	ValuePerDraw   float64 `json:"valuePerDraw"`
	PointsPerDraw  float64 `json:"pointsPerDraw"`
	RedrawsPerDraw float64 `json:"redrawsPerDraw"`
	// PityShare 长期有多大比例的抽奖是硬保底给的
	PityShare float64 `json:"pityShare"`
	// CycleMean 两次中奖之间平均隔几抽（E[T]）
	CycleMean float64      `json:"cycleMean"`
	Prizes    []ExactPrize `json:"prizes"`
	// Fortune 开了每日运气时：最倒霉、最走运两端与全体平均
	Fortune *ExactFortune `json:"fortune,omitempty"`
	// Notes 这次分析按固定状态处理、没有随时间演变的维度
	Notes []string `json:"notes"`
}

// ExactFortuneBand 某个运气系数下的综合数字。
type ExactFortuneBand struct {
	Factor       float64 `json:"factor"`
	WinRate      float64 `json:"winRate"`
	RealRate     float64 `json:"realRate"`
	ValuePerDraw float64 `json:"valuePerDraw"`
	// Prizes 各奖品的综合概率（与 ExactAnalysis.Prizes 同序）
	Prizes []float64 `json:"prizes"`
}

// ExactFortune 每日运气对综合概率的影响：两端，以及对运气分布积分之后的全体平均。
type ExactFortune struct {
	Low  ExactFortuneBand `json:"low"`
	High ExactFortuneBand `json:"high"`
	// Mean 全体用户的平均（对 z ~ N(0,1) 截在 ±2 的分布做高斯–勒让德求积，两端各一个质点）
	Mean ExactFortuneBand `json:"mean"`
}

// exactWithinDraws 给出「N 连抽至少出一个」的 N。
var exactWithinDraws = []int{1, 10, 50, 100}

const (
	exactSurvivalFloor = 1e-13
	// exactMaxSteps 首达分布最多逐步推进这么多步，之后按几何衰减外推
	exactMaxSteps = 4000
	// exactRatioTolerance 相邻两步衰减率的相对差小于它时认为进入准平稳
	exactRatioTolerance = 1e-10
)

// chainRow 一个连败状态上抽一次的分布。
type chainRow struct {
	probs []float64
	win   float64
	pity  bool
}

// lotteryChain 连败状态链：rows[k] 是已连续未中 k 次时的分布；最后一个状态的「未中」留在原地（尾部）。
type lotteryChain struct {
	rows []chainRow
	// weight 平稳分布的未归一权重 P(T > k)（最后一个状态并入了尾部）；cycle 是它们的和 E[T]
	weight []float64
	cycle  float64
	// stuck 链会以正概率永远抽不中（最后一个状态的中奖率为 0）
	stuck bool
}

func streakHorizon(activity Activity, strategy Strategy) int {
	horizon := 200
	if activity.PityThreshold > 0 {
		horizon = max(horizon, activity.PityThreshold+1)
	}
	if strategy.SoftPity != nil {
		horizon = max(horizon, strategy.SoftPity.Start+int(math.Ceil(100/math.Max(strategy.SoftPity.Step, 0.1)))+1)
	}
	return min(horizon, MaxStreakHorizon)
}

// buildChain 按同一套 PlanDraw / PityChoice 建链。
func buildChain(activity Activity, strategy Strategy, prizes []Prize, state DrawState) lotteryChain {
	horizon := streakHorizon(activity, strategy)
	chain := lotteryChain{}
	fallbackAt := -1
	if fallback, ok := FallbackPrize(prizes); ok {
		for index := range prizes {
			if prizes[index].ID == fallback.ID {
				fallbackAt = index
				break
			}
		}
	}
	survival := 1.0
	for k := 0; k < horizon && survival > exactSurvivalFloor; k++ {
		st := state
		st.MissStreak = k
		plan := PlanDraw(activity, strategy, prizes, st)
		row := chainRow{probs: make([]float64, len(prizes))}
		if index, pity := PityChoice(activity, plan, prizes, k); pity {
			row.probs[index], row.pity = 1, true
		} else {
			var total float64
			for _, odds := range plan.Odds {
				total += odds.Weight
			}
			switch {
			case total > 0:
				for index, odds := range plan.Odds {
					row.probs[index] = odds.Weight / total
				}
			case fallbackAt >= 0:
				// 所有权重都是 0：线上改发兜底奖
				row.probs[fallbackAt] = 1
			}
		}
		for index, p := range row.probs {
			if prizes[index].Winning() {
				row.win += p
			}
		}
		row.win = math.Min(1, row.win)
		chain.rows = append(chain.rows, row)
		chain.weight = append(chain.weight, survival)
		survival *= 1 - row.win
	}
	n := len(chain.rows)
	if n == 0 {
		return chain
	}
	if survival > exactSurvivalFloor {
		// 没走到「必中」：最后一个状态之后按它的分布继续，几何尾部并进它的权重
		last := chain.rows[n-1].win
		if last <= 0 {
			chain.stuck = true
		} else {
			chain.weight[n-1] += survival / last
		}
	}
	for _, w := range chain.weight {
		chain.cycle += w
	}
	return chain
}

// longRun 各奖品的综合概率。会永远抽不中的链，长期停在最后一个状态上。
func (c lotteryChain) longRun(count int) []float64 {
	out := make([]float64, count)
	if len(c.rows) == 0 {
		return out
	}
	if c.stuck {
		copy(out, c.rows[len(c.rows)-1].probs)
		return out
	}
	for k, row := range c.rows {
		share := c.weight[k] / c.cycle
		for index, p := range row.probs {
			out[index] += share * p
		}
	}
	return out
}

// pityShare 长期有多大比例的抽奖是硬保底给的。
func (c lotteryChain) pityShare() float64 {
	if c.stuck || c.cycle <= 0 {
		return 0
	}
	var share float64
	for k, row := range c.rows {
		if row.pity {
			share += c.weight[k] / c.cycle
		}
	}
	return share
}

// hitSteps 目标奖品在各状态上的吸收 / 回零 / 前进概率。
func (c lotteryChain) hitSteps(target int, winning bool) (absorb, reset, advance []float64) {
	n := len(c.rows)
	absorb, reset, advance = make([]float64, n), make([]float64, n), make([]float64, n)
	for k, row := range c.rows {
		a := row.probs[target]
		absorb[k] = a
		if winning {
			reset[k] = math.Max(0, row.win-a)
			advance[k] = math.Max(0, 1-row.win)
		} else {
			reset[k] = row.win
			advance[k] = math.Max(0, 1-row.win-a)
		}
	}
	return absorb, reset, advance
}

// hitMean 从状态 0 出发首次抽中目标奖品的期望次数；出不了时 ok = false。
func hitMean(reset, advance []float64) (float64, bool) {
	n := len(reset)
	// 最后一个状态的「前进」留在原地：t = 1 + r·t0 + d·t
	stay := advance[n-1]
	if stay >= 1 {
		// 走到这里就再也出不来；只有到不了这里时才有限，下面的 β 会体现
		stay = 1
	}
	alpha, beta := math.Inf(1), 0.0
	if stay < 1 {
		alpha, beta = 1/(1-stay), reset[n-1]/(1-stay)
	}
	for k := n - 2; k >= 0; k-- {
		if advance[k] == 0 {
			alpha, beta = 1, reset[k]
			continue
		}
		alpha, beta = 1+advance[k]*alpha, reset[k]+advance[k]*beta
	}
	if math.IsInf(alpha, 0) || math.IsNaN(alpha) || 1-beta <= 1e-15 {
		return 0, false
	}
	return alpha / (1 - beta), true
}

// hitDistribution 首达时间的分位数与 N 连抽概率：状态向量逐步推进，衰减率稳定后几何外推。
func hitDistribution(absorb, reset, advance []float64, quantiles []float64, within []int) ([]int, []float64) {
	n := len(absorb)
	qOut := make([]int, len(quantiles))
	wOut := make([]float64, len(within))
	vector := make([]float64, n)
	next := make([]float64, n)
	vector[0] = 1
	survival, ratio := 1.0, 0.0
	maxWithin := 0
	for _, w := range within {
		maxWithin = max(maxWithin, w)
	}
	pendingQ := len(quantiles)
	step := 0
	for step < exactMaxSteps && survival > exactSurvivalFloor {
		step++
		clear(next)
		var back float64
		for k, mass := range vector {
			if mass == 0 {
				continue
			}
			back += mass * reset[k]
			if k+1 < n {
				next[k+1] += mass * advance[k]
			} else {
				next[k] += mass * advance[k]
			}
		}
		next[0] += back
		var remaining float64
		for _, mass := range next {
			remaining += mass
		}
		vector, next = next, vector
		previous := survival
		survival = remaining
		for index, w := range within {
			if w == step {
				wOut[index] = 1 - survival
			}
		}
		for index, q := range quantiles {
			if qOut[index] == 0 && 1-survival >= q-1e-12 {
				qOut[index] = step
				pendingQ--
			}
		}
		if previous > 0 {
			current := survival / previous
			// 衰减率稳定：之后的每一步都是同一个比例，剩下的全部外推
			stable := step > n && ratio > 0 && math.Abs(current-ratio) <= exactRatioTolerance*ratio
			ratio = current
			if stable {
				break
			}
		}
		if pendingQ == 0 && step >= maxWithin {
			break
		}
	}
	// 几何外推：survival(step + j) = survival · ratio^j
	tail := func(draws int) float64 {
		if draws <= step || survival <= 0 || ratio <= 0 {
			return survival
		}
		return survival * math.Pow(ratio, float64(draws-step))
	}
	for index, w := range within {
		if w > step {
			wOut[index] = 1 - tail(w)
		}
	}
	for index, q := range quantiles {
		if qOut[index] != 0 {
			continue
		}
		if survival <= 1-q {
			qOut[index] = step
			continue
		}
		if ratio <= 0 || ratio >= 1 {
			continue
		}
		extra := math.Ceil(math.Log((1-q)/survival) / math.Log(ratio))
		if extra > 1e15 {
			continue
		}
		qOut[index] = step + int(math.Max(1, extra))
	}
	return qOut, wOut
}

// exactCore 综合概率与由它算出的几个汇总数（求解器每一轮只要这些，不要首达分布）。
type exactCore struct {
	longRun                                   []float64
	winRate, realRate, value, points, redraws float64
}

func coreOf(chain lotteryChain, activity Activity, strategy Strategy, prizes []Prize) exactCore {
	core := exactCore{longRun: chain.longRun(len(prizes))}
	for index, prize := range prizes {
		share := core.longRun[index]
		if prize.Winning() {
			core.winRate += share
			if !prize.Fallback {
				core.realRate += share
			}
		}
		core.value += share * PrizeValue(activity, strategy.Guard, prize)
		switch prize.Kind {
		case KindPoints:
			core.points += share * float64(prize.Reward.Amount)
		case KindLotteryDraws:
			core.redraws += share * float64(prize.Reward.Amount)
		}
	}
	return core
}

// AnalyzeExact 一个奖盘的精确分析。prizes 是这个人眼里的奖盘（已拥有剔除、中奖加成、运气都已乘好）。
func AnalyzeExact(activity Activity, strategy Strategy, prizes []Prize, state DrawState) ExactAnalysis {
	chain := buildChain(activity, strategy, prizes, state)
	out := ExactAnalysis{States: len(chain.rows), Prizes: make([]ExactPrize, len(prizes)), Notes: exactNotes(strategy, prizes)}
	base := Probabilities(prizes)
	core := coreOf(chain, activity, strategy, prizes)
	longRun := core.longRun
	out.WinRate, out.RealRate, out.ValuePerDraw = core.winRate, core.realRate, core.value
	out.PointsPerDraw, out.RedrawsPerDraw = core.points, core.redraws
	if !chain.stuck && chain.cycle > 0 {
		out.CycleMean = round6(chain.cycle)
	}
	out.PityShare = round6(chain.pityShare())
	for index, prize := range prizes {
		item := ExactPrize{
			PrizeID: prize.ID, Position: prize.Position, Name: prize.Name, Kind: prize.Kind,
			Nominal: base[index] / 100, LongRun: longRun[index],
			Value: PrizeValue(activity, strategy.Guard, prize), Within: make([]ExactWithin, len(exactWithinDraws)),
		}
		for w, draws := range exactWithinDraws {
			item.Within[w] = ExactWithin{Draws: draws}
		}
		if len(chain.rows) > 0 {
			absorb, reset, advance := chain.hitSteps(index, prize.Winning())
			if mean, ok := hitMean(reset, advance); ok && longRun[index] > 0 {
				item.HitMean = mean
				quantiles, within := hitDistribution(absorb, reset, advance, []float64{0.5, 0.9, 0.99}, exactWithinDraws)
				item.HitP50, item.HitP90, item.HitP99 = quantiles[0], quantiles[1], quantiles[2]
				for w := range within {
					item.Within[w].Probability = within[w]
				}
			} else {
				item.Never = true
			}
		} else {
			item.Never = true
		}
		out.Prizes[index] = item

		if prize.Winning() {
			out.BaseWinRate += base[index] / 100
			if !prize.Fallback {
				out.BaseRealRate += base[index] / 100
			}
		}
	}
	return out
}

// exactNotes 哪些维度在精确分析里按固定状态处理。
func exactNotes(strategy Strategy, prizes []Prize) []string {
	notes := []string{}
	limited, capped := false, false
	for _, prize := range prizes {
		if prize.Quantity >= 0 || prize.DailyLimit > 0 {
			limited = true
		}
		if prize.UserLimit > 0 || prize.UserDailyLimit > 0 {
			capped = true
		}
	}
	if limited {
		notes = append(notes, "库存与全站每日限量按此刻的余量固定处理，没有随时间消耗")
	}
	if capped {
		notes = append(notes, "每人上限按「还没中过」处理：重度用户中满之后，被挡掉的概率会分给其它奖品")
	}
	if strategy.Cooldown != nil {
		notes = append(notes, "中奖冷却依赖最近抽中的奖品，没有计入")
	}
	if strategy.Luck != nil {
		notes = append(notes, "运气平衡依赖整段历史，按此刻的运气状态固定处理")
	}
	if strategy.WinRate != nil || strategy.Budget != nil || strategy.Pacing != nil {
		notes = append(notes, "全站反馈、积分预算、匀速释放随全站进度变化，按此刻的进度固定处理")
	}
	return notes
}

// exactBand 某个运气系数下的综合数字。board 是乘运气之前的个人奖盘。
func exactBand(activity Activity, strategy Strategy, board []Prize, state DrawState, factor float64) ExactFortuneBand {
	prizes := FortunePrizes(board, factor)
	core := coreOf(buildChain(activity, strategy, prizes, state), activity, strategy, prizes)
	return ExactFortuneBand{Factor: factor, WinRate: core.winRate, RealRate: core.realRate,
		ValuePerDraw: core.value, Prizes: core.longRun}
}

// exactFortuneNodes 对运气分布求积用的高斯–勒让德节点数。被积函数（综合概率关于 z）光滑，16 个节点足够到 1e-9。
const exactFortuneNodes = 16

// AnalyzeExactFortune 每日运气对综合概率的影响。board 是乘运气之前的个人奖盘；grades 是这个人能得到的
// 至尊加成参数组（没有时为空）。没开每日运气时返回 nil。
//
// 全体平均是对 z 的分布积分：z ~ N(0,1)，截在 ±2（超出的部分落在两端，各占 Φ(−2) ≈ 2.28%）。
// 中间那一段用高斯–勒让德求积（gonum integrate/quad），两端各加一个质点。
func AnalyzeExactFortune(activity Activity, strategy Strategy, board []Prize, state DrawState, grades []SupremeGrade) *ExactFortune {
	fortune := strategy.Fortune
	if fortune == nil || fortune.Spread <= 0 {
		return nil
	}
	factorAt := func(z float64) float64 {
		natural := fortuneAt(fortune.Spread, z)
		if len(grades) > 0 {
			_, natural = BestSupreme(grades, natural)
		}
		return natural
	}
	out := &ExactFortune{
		Low:  exactBand(activity, strategy, board, state, factorAt(-fortuneClampZ)),
		High: exactBand(activity, strategy, board, state, factorAt(fortuneClampZ)),
	}
	normal := distuv.UnitNormal
	tailMass := normal.CDF(-fortuneClampZ)
	mean := ExactFortuneBand{Prizes: make([]float64, len(board))}
	accumulate := func(band ExactFortuneBand, mass float64) {
		mean.Factor += band.Factor * mass
		mean.WinRate += band.WinRate * mass
		mean.RealRate += band.RealRate * mass
		mean.ValuePerDraw += band.ValuePerDraw * mass
		for index, p := range band.Prizes {
			mean.Prizes[index] += p * mass
		}
	}
	accumulate(out.Low, tailMass)
	accumulate(out.High, tailMass)
	nodes := make([]float64, exactFortuneNodes)
	weights := make([]float64, exactFortuneNodes)
	quad.Legendre{}.FixedLocations(nodes, weights, -fortuneClampZ, fortuneClampZ)
	for index, z := range nodes {
		accumulate(exactBand(activity, strategy, board, state, factorAt(z)), weights[index]*normal.Prob(z))
	}
	out.Mean = mean
	return out
}
