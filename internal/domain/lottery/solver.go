package lottery

import (
	"fmt"
	"math"
	"slices"
	"sort"

	"gonum.org/v1/gonum/mat"
)

// 奖盘求解：把「我想要什么样的奖盘」反过来解成权重。
//
// 调奖盘一直是手工活：想让综合中奖率到 35%、每抽成本不超过 8 积分、月度会员的综合概率定在 0.1%，
// 只能一格一格改权重、跑模拟、再改。几个目标互相牵扯（提高一格，别的格都跟着变；保底、软保底、
// 中奖加成、每日运气又让实际概率偏离权重占比），靠手调很难同时满足。
//
// ── 问题 ──
//
// 记基础奖盘的权重占比为 q，要求的新占比为 p。目标都是对**综合概率**提的（求解对象眼里的、含保底的
// 长期概率，见 exact.go）：综合中奖率、真实奖品率、每抽期望价值、每抽期望积分回报、单个奖品的综合概率。
// 在满足全部目标的 p 里，取与 q 的 KL 散度最小的那个 —— 对现有奖盘改动最少、不凭空引入别的偏好
// （最小鉴别信息原理，与护栏的 I-投影同一个原则）。
//
// ── 两层求解 ──
//
// 内层：目标如果直接写在权重占比上，就是 p 的线性约束，I-投影有唯一解（projection.go，凸对偶上的
// 投影牛顿法，等式约束的对偶变量不限符号）。
//
// 外层：综合概率 m 不是占比 x 的线性函数（保底把中奖率抬高、中奖加成与运气把真实奖品放大）。
// 把每条目标在占比上的右端 x_j 当未知数，解方程组 m(x) = t。两者之间近似是「各自乘一个系数」的关系，
// 所以在对数坐标上解：y = ln|x|，G(y) = ln m − ln t，用 Broyden 拟牛顿法（初始雅可比取单位阵，
// 第一步就是按比例修正；之后用割线信息修正各目标之间的耦合），带回溯。
// 每一轮的综合概率都由精确分析给出，不抽样，所以解到 1e-9 没有噪声。
//
// 不等式目标（期望价值上限、积分回报上限、概率区间）按有效集处理：在占比上不紧、综合值也没越界的
// 这一轮不动；越界了就把右端拉到「刚好紧」再参与求解。
//
// ── 落到整数 ──
//
// 权重是整数。解出来的占比按最大余数法取整到合计 Total（概率大于 0 的至少 1），再用取整后的权重
// 重算一遍精确分析：结果里的数字是整数权重真实的综合概率，不是连续解的。

// 单个奖品的目标方式。
const (
	// SolveLock 权重占比保持不变
	SolveLock = "lock"
	// SolveTarget 综合概率等于 Probability
	SolveTarget = "target"
	// SolveRange 综合概率在 [Min, Max] 之内（Max 为 0 表示不设上限）
	SolveRange = "range"
)

// 期望价值目标的方式。
const (
	SolveValueMax   = "max"
	SolveValueEqual = "equal"
)

// 求解的规模与默认值。
const (
	DefaultSolveTotal = 100_000
	MaxSolveTotal     = MaxWeight
	solveIterations   = 60
	solveTolerance    = 1e-9
	// solveScale 求解过程中把占比放大成整数权重的倍数（越大越精细；乘上中奖加成与运气的放大倍数仍在 int64 内）
	solveScale = 1e12
)

// SolvePrizeGoal 一个奖品的目标。
type SolvePrizeGoal struct {
	Position    int     `json:"position"`
	Mode        string  `json:"mode"`
	Probability float64 `json:"probability,omitempty"`
	Min         float64 `json:"min,omitempty"`
	Max         float64 `json:"max,omitempty"`
}

// SolveGoal 求解目标。每一项都可以不设（0）；全部概率是 0–1。
type SolveGoal struct {
	// WinRate 综合中奖率
	WinRate float64 `json:"winRate,omitempty"`
	// RealRate 综合概率里抽中兜底奖、谢谢参与以外奖品的部分（没有「谢谢参与」的奖盘用它当中奖率）
	RealRate float64 `json:"realRate,omitempty"`
	// Value 每抽期望价值（积分当量）；ValueMode 为 max（上限，默认）或 equal
	Value     float64 `json:"value,omitempty"`
	ValueMode string  `json:"valueMode,omitempty"`
	// PointsReturn 每抽期望积分回报的上限：P / (1 − Q)，再得的抽奖次数接着抽
	PointsReturn float64 `json:"pointsReturn,omitempty"`
	// Prizes 单个奖品的目标
	Prizes []SolvePrizeGoal `json:"prizes,omitempty"`
	// Total 新权重的合计；0 为自动（沿用现在的合计，不够精细时放大）
	Total int `json:"total,omitempty"`
}

// SolveModel 求解对象：基础奖盘，以及把它变成「求解对象眼里的活动与奖盘」的个人化过程。
type SolveModel struct {
	Activity Activity
	Strategy Strategy
	// Prizes 基础奖盘（活动上保存的权重）
	Prizes []Prize
	State  DrawState
	// Personalize 已拥有剔除、中奖加成、运气；为 nil 时目标直接提在基础奖盘上
	Personalize func(Activity, []Prize) (Activity, []Prize)
}

// SolveCheck 一条目标的达成情况。
type SolveCheck struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Mode equal / max / min / lock
	Mode   string  `json:"mode"`
	Target float64 `json:"target"`
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	Met    bool    `json:"met"`
	// Position 针对单个奖品的目标时是它的位置，否则为 -1
	Position int `json:"position"`
}

// SolvedPrize 一个奖品求解前后的权重与概率。
type SolvedPrize struct {
	PrizeID   int64  `json:"prizeId"`
	Position  int    `json:"position"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Weight    int    `json:"weight"`
	NewWeight int    `json:"newWeight"`
	// Nominal / NewNominal 基础奖盘的公示概率（权重占比，0–1）
	Nominal    float64 `json:"nominal"`
	NewNominal float64 `json:"newNominal"`
	// LongRun / NewLongRun 求解对象眼里的综合概率（0–1）
	LongRun    float64 `json:"longRun"`
	NewLongRun float64 `json:"newLongRun"`
}

// SolveSummary 求解对象眼里的综合数字。
type SolveSummary struct {
	WinRate        float64 `json:"winRate"`
	RealRate       float64 `json:"realRate"`
	ValuePerDraw   float64 `json:"valuePerDraw"`
	PointsPerDraw  float64 `json:"pointsPerDraw"`
	RedrawsPerDraw float64 `json:"redrawsPerDraw"`
	// PointsReturn 每抽期望积分回报 P / (1 − Q)；再得次数的期望不小于 1 时为 -1（抽不完）
	PointsReturn float64 `json:"pointsReturn"`
}

// SolveResult 求解结果。
type SolveResult struct {
	// Feasible 目标之间不矛盾、在现有奖品上可以同时满足；Converged 外层解到了精度以内
	Feasible   bool `json:"feasible"`
	Converged  bool `json:"converged"`
	Iterations int  `json:"iterations"`
	// Total 新权重的合计
	Total int `json:"total"`
	// Divergence 新旧权重占比的 KL 散度（奈特）：改动幅度，0 为没改
	Divergence float64       `json:"divergence"`
	Prizes     []SolvedPrize `json:"prizes"`
	Before     SolveSummary  `json:"before"`
	After      SolveSummary  `json:"after"`
	Checks     []SolveCheck  `json:"checks"`
	Notes      []string      `json:"notes"`
}

// ValidateSolveGoal 校验目标。prizes 是基础奖盘。
func ValidateSolveGoal(goal SolveGoal, prizes []Prize) error {
	rate := func(name string, value float64) error {
		if value < 0 || value >= 1 || math.IsNaN(value) {
			return fmt.Errorf("%s需要在 0–100%% 之间", name)
		}
		return nil
	}
	if err := rate("综合中奖率", goal.WinRate); err != nil {
		return err
	}
	if err := rate("真实奖品率", goal.RealRate); err != nil {
		return err
	}
	if goal.Value < 0 || goal.PointsReturn < 0 || math.IsNaN(goal.Value) || math.IsNaN(goal.PointsReturn) {
		return fmt.Errorf("期望价值与积分回报不能为负")
	}
	if goal.ValueMode != "" && goal.ValueMode != SolveValueMax && goal.ValueMode != SolveValueEqual {
		return fmt.Errorf("未知的期望价值方式：%s", goal.ValueMode)
	}
	if goal.Total != 0 && (goal.Total < 100 || goal.Total > MaxSolveTotal) {
		return fmt.Errorf("权重合计需要在 100–%d 之间", MaxSolveTotal)
	}
	hasNone := false
	for _, prize := range prizes {
		if !prize.Winning() {
			hasNone = true
		}
	}
	if goal.WinRate > 0 && !hasNone {
		return fmt.Errorf("奖盘没有「谢谢参与」，每次都中奖：请改用真实奖品率（兜底奖以外的概率）作目标")
	}
	seen := map[int]bool{}
	for _, item := range goal.Prizes {
		index := slices.IndexFunc(prizes, func(p Prize) bool { return p.Position == item.Position })
		if index < 0 {
			return fmt.Errorf("奖品位置 %d 不在奖盘上", item.Position)
		}
		if seen[item.Position] {
			return fmt.Errorf("位置 %d 的奖品设了不止一个目标", item.Position+1)
		}
		seen[item.Position] = true
		label := fmt.Sprintf("「%s」", prizeLabel(prizes[index]))
		switch item.Mode {
		case SolveLock:
		case SolveTarget:
			if item.Probability <= 0 || item.Probability >= 1 {
				return fmt.Errorf("%s的目标概率需要大于 0、小于 100%%", label)
			}
		case SolveRange:
			if item.Min < 0 || item.Max < 0 || item.Min >= 1 || item.Max >= 1 {
				return fmt.Errorf("%s的概率区间需要在 0–100%% 之间", label)
			}
			if item.Min <= 0 && item.Max <= 0 {
				return fmt.Errorf("%s的概率区间至少要填一端", label)
			}
			if item.Max > 0 && item.Min > item.Max {
				return fmt.Errorf("%s的概率下限不能高于上限", label)
			}
		default:
			return fmt.Errorf("%s的目标方式 %s 不认识", label, item.Mode)
		}
	}
	return nil
}

func prizeLabel(prize Prize) string {
	if prize.Name != "" {
		return prize.Name
	}
	return fmt.Sprintf("位置 %d", prize.Position+1)
}

// solveRow 一条目标：在占比上是 a·p (= / ≤) x，综合值由 measure 给出。
type solveRow struct {
	check SolveCheck
	// a 占比上的系数；eq 等式
	a  []float64
	eq bool
	// target 综合值的目标；scale 归一用的量级（|target|）
	target float64
	scale  float64
	// measure 这条目标的综合值（与 target 同一个口径，下限取了负号）；nil 表示目标就提在占比上（锁定），不用外层校准
	measure func(exactCore) float64
	// shown 给人看的综合值（下限换回正数、积分回报换成 P / (1 − Q)）；nil 时就是 measure
	shown func(exactCore) float64
}

// amax 系数的最大绝对值：一个权重单位最多让这条目标的值变化 amax / 合计。
func (r solveRow) amax() float64 {
	var worst float64
	for _, v := range r.a {
		worst = math.Max(worst, math.Abs(v))
	}
	return worst
}

func (r solveRow) show(core exactCore) float64 {
	if r.shown != nil {
		return r.shown(core)
	}
	return r.measure(core)
}

// SolveWheel 按目标解出新的权重。目标自相矛盾时 Feasible 为假，给出尽力而为的结果。
func SolveWheel(model SolveModel, goal SolveGoal) SolveResult {
	prizes := model.Prizes
	n := len(prizes)
	result := SolveResult{Prizes: make([]SolvedPrize, n), Checks: []SolveCheck{}, Notes: []string{}}

	// 现在的占比 q；给零权重却定了目标概率的奖品一个起点，否则 I-投影不会凭空分给它概率
	q := make([]float64, n)
	var weightSum float64
	for index, prize := range prizes {
		q[index] = float64(max(prize.Weight, 0))
		weightSum += q[index]
	}
	if weightSum <= 0 {
		result.Notes = append(result.Notes, "奖盘的权重合计为 0，无法求解")
		return result
	}
	for index := range q {
		q[index] /= weightSum
	}
	original := slices.Clone(q)
	for _, item := range goal.Prizes {
		index := slices.IndexFunc(prizes, func(p Prize) bool { return p.Position == item.Position })
		if index < 0 || q[index] > 0 {
			continue
		}
		seed := item.Probability
		if item.Mode == SolveRange {
			seed = item.Min
		}
		if item.Mode == SolveLock || seed <= 0 {
			continue
		}
		q[index] = seed
		result.Notes = append(result.Notes, fmt.Sprintf("「%s」原来不参与常规抽取，为达到目标概率给了它权重", prizeLabel(prizes[index])))
	}
	normalize(q)

	evaluate := func(p []float64) exactCore {
		board := slices.Clone(prizes)
		for index := range board {
			board[index].Weight = int(math.Round(p[index] * solveScale))
		}
		return solveCore(model, board)
	}
	before := solveCore(model, prizes)
	result.Before = summaryOf(before)

	// 期望价值按求解对象眼里的活动估（特权会改积分抽奖价，抽奖次数的估值随之变）
	valued := model.Activity
	if model.Personalize != nil {
		valued, _ = model.Personalize(model.Activity, prizes)
	}
	rows := solveRows(goal, prizes, original, func(prize Prize) float64 {
		return PrizeValue(valued, model.Strategy.Guard, prize)
	})
	if len(rows) == 0 {
		result.Notes = append(result.Notes, "没有设任何目标，权重保持不变")
		result.Feasible, result.Converged, result.After = true, true, result.Before
		for index, prize := range prizes {
			result.Total += max(prize.Weight, 0)
			result.Prizes[index] = SolvedPrize{
				PrizeID: prize.ID, Position: prize.Position, Name: prize.Name, Kind: prize.Kind,
				Weight: prize.Weight, NewWeight: prize.Weight, Nominal: original[index], NewNominal: original[index],
				LongRun: before.longRun[index], NewLongRun: before.longRun[index],
			}
		}
		return result
	}
	for index := range rows {
		if rows[index].measure != nil {
			rows[index].check.Before = rows[index].show(before)
		}
	}

	// 外层：对数坐标上的 Broyden。x 是各目标在占比上的右端，初值取目标本身
	m := len(rows)
	x := make([]float64, m)
	for j, row := range rows {
		x[j] = row.target
	}
	project := func(x []float64) ([]float64, bool) {
		constraints := make([]linearConstraint, m)
		for j, row := range rows {
			a := make([]float64, n)
			for i := range a {
				a[i] = row.a[i] / row.scale
			}
			constraints[j] = linearConstraint{a: a, b: x[j] / row.scale, eq: row.eq}
		}
		p, feasible := iProject(q, constraints)
		normalize(p)
		return p, feasible
	}
	// residual 各目标在对数坐标上的残差；frozen 的这一轮不参与（不等式在占比上不紧、综合值也没越界）
	residual := func(p []float64, core exactCore, x []float64) ([]float64, []bool) {
		g := make([]float64, m)
		frozen := make([]bool, m)
		for j, row := range rows {
			if row.measure == nil {
				frozen[j] = true
				continue
			}
			value := row.measure(core)
			over := value > row.target+math.Abs(row.target)*solveTolerance
			if value*row.target <= 0 {
				// 综合值为 0 或与目标异号（要求的概率一点都抽不到）：取不了对数。
				// 已满足的不等式不动；其余按固定残差推着右端往大走
				if !row.eq && !over {
					frozen[j] = true
				} else {
					g[j] = -1
				}
				continue
			}
			// 对数残差对正负目标都成立：下限取了负号，value / target 仍是正数
			g[j] = math.Log(value / row.target)
			if row.eq {
				continue
			}
			// a·p ≤ x：综合值没越界、在占比上也不紧的这一轮不动
			var nominal float64
			for i := range p {
				nominal += row.a[i] * p[i]
			}
			active := nominal >= x[j]-1e-9*row.scale
			if !over && !active {
				frozen[j], g[j] = true, 0
			}
		}
		return g, frozen
	}
	norm := func(g []float64, frozen []bool) float64 {
		var worst float64
		for j := range g {
			if !frozen[j] {
				worst = math.Max(worst, math.Abs(g[j]))
			}
		}
		return worst
	}

	p, feasible := project(x)
	core := evaluate(p)
	g, frozen := residual(p, core, x)
	jacobian := mat.NewDense(max(m, 1), max(m, 1), nil)
	resetJacobian := func() {
		jacobian.Zero()
		for j := range m {
			jacobian.Set(j, j, 1)
		}
	}
	resetJacobian()
	result.Feasible = feasible
	for iteration := 0; iteration < solveIterations && m > 0; iteration++ {
		current := norm(g, frozen)
		if current < solveTolerance {
			result.Converged = true
			break
		}
		result.Iterations = iteration + 1
		// 在占比上不紧、但综合值越界的不等式：先把右端拉到刚好紧的位置
		for j, row := range rows {
			if row.eq || row.measure == nil || frozen[j] {
				continue
			}
			var nominal float64
			for i := range p {
				nominal += row.a[i] * p[i]
			}
			if nominal < x[j]-1e-9*row.scale && nominal*row.target > 0 {
				// 右端换了起点，之前攒的割线信息不再对得上
				x[j] = nominal
				resetJacobian()
			}
		}
		free := make([]int, 0, m)
		for j := range m {
			if !frozen[j] {
				free = append(free, j)
			}
		}
		// J · d = −g（只在参与的目标上）
		k := len(free)
		step := make([]float64, m)
		if k > 0 {
			sub := mat.NewDense(k, k, nil)
			rhs := mat.NewVecDense(k, nil)
			for a, j := range free {
				rhs.SetVec(a, -g[j])
				for b, l := range free {
					sub.Set(a, b, jacobian.At(j, l))
				}
			}
			var direction mat.VecDense
			if err := direction.SolveVec(sub, rhs); err != nil {
				resetJacobian()
				for _, j := range free {
					step[j] = -g[j]
				}
			} else {
				for a, j := range free {
					step[j] = direction.AtVec(a)
				}
			}
		}
		// 回溯：残差没有变小就把步长减半（对数坐标上一步最多走 e³ 倍）
		var (
			nextX      []float64
			nextP      []float64
			nextCore   exactCore
			nextG      []float64
			nextFrozen []bool
			nextOK     bool
		)
		accepted := false
		scale := 1.0
		for range 12 {
			candidate := slices.Clone(x)
			for _, j := range free {
				candidate[j] = x[j] * math.Exp(math.Max(-3, math.Min(3, scale*step[j])))
			}
			nextP, nextOK = project(candidate)
			nextCore = evaluate(nextP)
			nextG, nextFrozen = residual(nextP, nextCore, candidate)
			nextX = candidate
			if norm(nextG, nextFrozen) < current*(1-1e-4*scale) {
				accepted = true
				break
			}
			scale /= 2
		}
		if !accepted {
			// 走不动了：目标之间矛盾，或已经到了这组奖品能达到的极限
			break
		}
		// Broyden 更新（只在有效集没变时；变了就从单位阵重来）
		same := true
		for j := range m {
			if frozen[j] != nextFrozen[j] {
				same = false
			}
		}
		if same && k > 0 {
			dx := mat.NewVecDense(m, nil)
			dg := mat.NewVecDense(m, nil)
			var dot float64
			for j := range m {
				if frozen[j] {
					continue
				}
				d := math.Log(nextX[j] / x[j])
				dx.SetVec(j, d)
				dg.SetVec(j, nextG[j]-g[j])
				dot += d * d
			}
			if dot > 1e-30 {
				var jdx mat.VecDense
				jdx.MulVec(jacobian, dx)
				dg.SubVec(dg, &jdx)
				var update mat.Dense
				update.Outer(1/dot, dg, dx)
				jacobian.Add(jacobian, &update)
			}
		} else {
			resetJacobian()
		}
		x, p, core, g, frozen, feasible = nextX, nextP, nextCore, nextG, nextFrozen, nextOK
		result.Feasible = feasible
	}
	if m > 0 && norm(g, frozen) < solveTolerance {
		result.Converged = true
	}

	// 整数化，再按整数权重重算
	total := solveTotal(goal.Total, prizes, p)
	weights := integerWeights(p, total)
	result.Total = 0
	for _, w := range weights {
		result.Total += w
	}
	solved := slices.Clone(prizes)
	finalShare := make([]float64, n)
	for index := range solved {
		solved[index].Weight = weights[index]
		finalShare[index] = float64(weights[index]) / float64(max(result.Total, 1))
	}
	after := solveCore(model, solved)
	result.After = summaryOf(after)
	for index, prize := range prizes {
		result.Prizes[index] = SolvedPrize{
			PrizeID: prize.ID, Position: prize.Position, Name: prize.Name, Kind: prize.Kind,
			Weight: prize.Weight, NewWeight: weights[index],
			Nominal: original[index], NewNominal: finalShare[index],
			LongRun: before.longRun[index], NewLongRun: after.longRun[index],
		}
		if original[index] > 0 && finalShare[index] > 0 {
			result.Divergence += finalShare[index] * math.Log(finalShare[index]/original[index])
		}
	}
	result.Divergence = math.Max(0, result.Divergence)

	// 达成情况按整数权重的真实值判。容差：目标的 0.5%，与几个权重单位能带来的变化（取整误差）两者取大
	unit := 1 / float64(max(result.Total, 1))
	for _, row := range rows {
		check := row.check
		var value float64
		if row.measure != nil {
			value = row.measure(after)
			check.After = row.show(after)
		} else {
			for i := range finalShare {
				value += row.a[i] * finalShare[i]
			}
			check.After = value
		}
		slack := math.Max(math.Abs(row.target)*5e-3, 4*unit*row.amax())
		if row.eq {
			check.Met = math.Abs(value-row.target) <= slack
		} else {
			check.Met = value <= row.target+slack
		}
		result.Checks = append(result.Checks, check)
		if !check.Met {
			result.Feasible = false
		}
	}
	if !result.Feasible {
		result.Notes = append(result.Notes, "目标之间互相矛盾，或在现有奖品上达不到：下面是最接近的一组权重，未达成的目标已标出")
	}
	if after.redraws >= 1 {
		result.Notes = append(result.Notes, "抽奖次数奖品的期望不小于 1 次：抽一次期望再得一次以上，能一直抽下去")
	}
	return result
}

// solveCore 求解对象眼里的综合数字（先个人化，再建链）。
func solveCore(model SolveModel, base []Prize) exactCore {
	activity, prizes := model.Activity, base
	if model.Personalize != nil {
		activity, prizes = model.Personalize(activity, base)
	}
	return coreOf(buildChain(activity, model.Strategy, prizes, model.State), activity, model.Strategy, prizes)
}

func summaryOf(core exactCore) SolveSummary {
	summary := SolveSummary{
		WinRate: core.winRate, RealRate: core.realRate, ValuePerDraw: core.value,
		PointsPerDraw: core.points, RedrawsPerDraw: core.redraws, PointsReturn: -1,
	}
	if core.redraws < 1 {
		summary.PointsReturn = core.points / (1 - core.redraws)
	}
	return summary
}

// solveRows 把目标翻译成约束行。share 是现在的权重占比，valueOf 是奖品的积分当量。
func solveRows(goal SolveGoal, prizes []Prize, share []float64, valueOf func(Prize) float64) []solveRow {
	n := len(prizes)
	rows := []solveRow{}
	indicator := func(match func(Prize) bool) []float64 {
		a := make([]float64, n)
		for i, prize := range prizes {
			if match(prize) {
				a[i] = 1
			}
		}
		return a
	}
	if goal.WinRate > 0 {
		rows = append(rows, solveRow{
			check: SolveCheck{Key: "winRate", Label: "综合中奖率", Mode: "equal", Target: goal.WinRate, Position: -1},
			a:     indicator(func(p Prize) bool { return p.Winning() }),
			eq:    true, target: goal.WinRate, scale: goal.WinRate,
			measure: func(core exactCore) float64 { return core.winRate },
		})
	}
	if goal.RealRate > 0 {
		rows = append(rows, solveRow{
			check: SolveCheck{Key: "realRate", Label: "真实奖品率", Mode: "equal", Target: goal.RealRate, Position: -1},
			a:     indicator(func(p Prize) bool { return p.Winning() && !p.Fallback }),
			eq:    true, target: goal.RealRate, scale: goal.RealRate,
			measure: func(core exactCore) float64 { return core.realRate },
		})
	}
	if goal.Value > 0 {
		a := make([]float64, n)
		for i, prize := range prizes {
			a[i] = valueOf(prize)
		}
		mode := "max"
		if goal.ValueMode == SolveValueEqual {
			mode = "equal"
		}
		rows = append(rows, solveRow{
			check: SolveCheck{Key: "value", Label: "每抽期望价值", Mode: mode, Target: goal.Value, Position: -1},
			a:     a, eq: mode == "equal", target: goal.Value, scale: goal.Value,
			measure: func(core exactCore) float64 { return core.value },
		})
	}
	if goal.PointsReturn > 0 {
		// P / (1 − Q) ≤ R  ⇔  P + R·Q ≤ R：对综合概率是线性的
		limit := goal.PointsReturn
		a := make([]float64, n)
		for i, prize := range prizes {
			switch prize.Kind {
			case KindPoints:
				a[i] = float64(prize.Reward.Amount)
			case KindLotteryDraws:
				a[i] = limit * float64(prize.Reward.Amount)
			}
		}
		rows = append(rows, solveRow{
			check: SolveCheck{Key: "pointsReturn", Label: "每抽期望积分回报", Mode: "max", Target: limit, Position: -1},
			a:     a, target: limit, scale: limit,
			measure: func(core exactCore) float64 { return core.points + limit*core.redraws },
			shown:   func(core exactCore) float64 { return summaryOf(core).PointsReturn },
		})
	}
	for _, item := range goal.Prizes {
		index := slices.IndexFunc(prizes, func(p Prize) bool { return p.Position == item.Position })
		if index < 0 {
			continue
		}
		unit := make([]float64, n)
		unit[index] = 1
		name := prizeLabel(prizes[index])
		at := index
		longRun := func(core exactCore) float64 { return core.longRun[at] }
		switch item.Mode {
		case SolveLock:
			if share[index] <= 0 {
				continue
			}
			rows = append(rows, solveRow{
				check: SolveCheck{Key: "lock", Label: name + " 占比不变", Mode: "lock", Target: share[index],
					Before: share[index], Position: item.Position},
				a: unit, eq: true, target: share[index], scale: share[index],
			})
		case SolveTarget:
			rows = append(rows, solveRow{
				check: SolveCheck{Key: "prize", Label: name + " 综合概率", Mode: "equal", Target: item.Probability, Position: item.Position},
				a:     unit, eq: true, target: item.Probability, scale: item.Probability, measure: longRun,
			})
		case SolveRange:
			if item.Max > 0 {
				rows = append(rows, solveRow{
					check: SolveCheck{Key: "prizeMax", Label: name + " 综合概率上限", Mode: "max", Target: item.Max, Position: item.Position},
					a:     unit, target: item.Max, scale: item.Max, measure: longRun,
				})
			}
			if item.Min > 0 {
				// 下限写成 −p ≤ −min，与上限同一种形状
				lower := make([]float64, n)
				lower[index] = -1
				rows = append(rows, solveRow{
					check: SolveCheck{Key: "prizeMin", Label: name + " 综合概率下限", Mode: "min", Target: item.Min, Position: item.Position},
					a:     lower, target: -item.Min, scale: item.Min,
					measure: func(core exactCore) float64 { return -core.longRun[at] },
					shown:   longRun,
				})
			}
		}
	}
	return rows
}

// solveTotal 新权重的合计：指定了就用指定的；否则沿用现在的合计，最小的非零概率不足 20 个权重单位时放大十倍，直到上限。
func solveTotal(requested int, prizes []Prize, p []float64) int {
	if requested > 0 {
		return requested
	}
	total := 0
	for _, prize := range prizes {
		total += max(prize.Weight, 0)
	}
	if total < 1000 {
		total = DefaultSolveTotal
	}
	total = min(total, MaxSolveTotal)
	smallest := 1.0
	for _, share := range p {
		if share > 0 {
			smallest = math.Min(smallest, share)
		}
	}
	for total*10 <= MaxSolveTotal && smallest*float64(total) < 20 {
		total *= 10
	}
	return total
}

// integerWeights 最大余数法：占比 → 合计为 total 的整数权重，概率大于 0 的至少 1。
func integerWeights(p []float64, total int) []int {
	n := len(p)
	weights := make([]int, n)
	type remainder struct {
		index int
		frac  float64
	}
	rest := make([]remainder, 0, n)
	assigned := 0
	for index, share := range p {
		if share <= 0 {
			continue
		}
		exact := share * float64(total)
		weights[index] = int(math.Floor(exact))
		rest = append(rest, remainder{index, exact - math.Floor(exact)})
		assigned += weights[index]
	}
	sort.SliceStable(rest, func(a, b int) bool { return rest[a].frac > rest[b].frac })
	for i := 0; assigned < total && len(rest) > 0; i++ {
		weights[rest[i%len(rest)].index]++
		assigned++
	}
	// 概率大于 0 却被取整成 0 的补 1，从最大的那一格里扣
	for index, share := range p {
		if share <= 0 || weights[index] > 0 {
			continue
		}
		largest := 0
		for j := range weights {
			if weights[j] > weights[largest] {
				largest = j
			}
		}
		if weights[largest] > 1 {
			weights[largest]--
			weights[index] = 1
		}
	}
	return weights
}

func normalize(values []float64) {
	var sum float64
	for _, v := range values {
		sum += v
	}
	if sum <= 0 {
		return
	}
	for index := range values {
		values[index] /= sum
	}
}
