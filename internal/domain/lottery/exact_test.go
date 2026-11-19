package lottery

import (
	"math"
	"testing"
	"time"

	"gonum.org/v1/gonum/mat"
)

// wheelPrizes 一张没有「谢谢参与」的转盘（每次都中奖，10 积分兜底），接近线上的奖盘。权重合计 100000。
func wheelPrizes() []Prize {
	points := func(id int64, position int, amount int64, weight int) Prize {
		return Prize{ID: id, Position: position, Name: "积分", Kind: KindPoints, Reward: Reward{Type: KindPoints, Amount: amount},
			Weight: weight, Quantity: -1}
	}
	prizes := []Prize{
		points(1, 0, 10, 70_000),
		{ID: 2, Position: 1, Name: "永久会员", Kind: KindVipLifetime, Reward: Reward{Type: KindVipLifetime}, Weight: 1, Quantity: -1, Value: 20_000},
		{ID: 3, Position: 2, Name: "月度会员", Kind: KindVipDays, Reward: Reward{Type: KindVipDays, Amount: 30}, Weight: 2_000, Quantity: -1},
		{ID: 4, Position: 3, Name: "会员 15 天", Kind: KindVipDays, Reward: Reward{Type: KindVipDays, Amount: 15}, Weight: 4_000, Quantity: -1},
		points(5, 4, 50, 3_000),
		points(6, 5, 288, 600),
		{ID: 7, Position: 6, Name: "抽奖次数", Kind: KindLotteryDraws, Reward: Reward{Type: KindLotteryDraws, Amount: 2}, Weight: 6_000, Quantity: -1},
		{ID: 8, Position: 7, Name: "免广告 15 天", Kind: KindAdFree, Reward: Reward{Type: KindAdFree, Amount: 15}, Weight: 14_399, Quantity: -1},
	}
	prizes[0].Fallback = true
	prizes[0].Name = "10 积分"
	return prizes
}

// 独立抽取：综合概率就是公示概率，首达时间服从几何分布。
func TestAnalyzeExactGeometric(t *testing.T) {
	prizes := strategyPrizes()
	a := AnalyzeExact(Activity{}, Strategy{}, prizes, DrawState{Now: time.Now()})
	if a.States != 200 && a.States < 30 {
		t.Fatalf("状态数 %d", a.States)
	}
	if math.Abs(a.WinRate-0.44) > 1e-9 || math.Abs(a.BaseWinRate-0.44) > 1e-9 || a.PityShare != 0 {
		t.Fatalf("中奖率 %v / %v，保底占比 %v", a.WinRate, a.BaseWinRate, a.PityShare)
	}
	if math.Abs(a.CycleMean-1/0.44) > 1e-5 {
		t.Fatalf("周期 %v", a.CycleMean)
	}
	var sum float64
	for index, prize := range a.Prizes {
		sum += prize.LongRun
		if math.Abs(prize.LongRun-prize.Nominal) > 1e-9 || math.Abs(prize.Nominal-float64(prizes[index].Weight)/100) > 1e-9 {
			t.Fatalf("%d 号综合 %v、公示 %v", prize.PrizeID, prize.LongRun, prize.Nominal)
		}
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatalf("综合概率之和 %v", sum)
	}
	// 30% 的那一格：期望 1/0.3 抽，中位 2，十连至少一个 1 − 0.7¹⁰
	p := a.Prizes[1]
	if math.Abs(p.HitMean-1/0.3) > 1e-6 || p.HitP50 != 2 || p.HitP90 != 7 || p.HitP99 != 13 {
		t.Fatalf("几何分布不对：%+v", p)
	}
	if math.Abs(p.Within[0].Probability-0.3) > 1e-9 || math.Abs(p.Within[1].Probability-(1-math.Pow(0.7, 10))) > 1e-9 {
		t.Fatalf("N 连概率不对：%+v", p.Within)
	}
	// 2 积分 ×30% + 5 ×10% + 15 ×3%
	if math.Abs(a.PointsPerDraw-(0.6+0.5+0.45)) > 1e-9 {
		t.Fatalf("每抽积分 %v", a.PointsPerDraw)
	}
}

// 十万分之一的大奖：分位数靠几何外推，不用真的推几十万步。
func TestAnalyzeExactRarePrize(t *testing.T) {
	a := AnalyzeExact(Activity{}, Strategy{}, wheelPrizes(), DrawState{Now: time.Now()})
	if a.States != 1 || math.Abs(a.WinRate-1) > 1e-12 || math.Abs(a.RealRate-0.3) > 1e-9 {
		t.Fatalf("没有谢谢参与的奖盘只有一个状态、每次都中：%+v", a)
	}
	rare := a.Prizes[1]
	p := 1e-5
	if math.Abs(rare.HitMean-1/p)/(1/p) > 1e-9 {
		t.Fatalf("期望抽数 %v", rare.HitMean)
	}
	want := func(q float64) int { return int(math.Ceil(math.Log(1-q) / math.Log(1-p))) }
	if rare.HitP50 != want(0.5) || rare.HitP90 != want(0.9) || rare.HitP99 != want(0.99) {
		t.Fatalf("分位数 %d/%d/%d，应为 %d/%d/%d", rare.HitP50, rare.HitP90, rare.HitP99, want(0.5), want(0.9), want(0.99))
	}
	if math.Abs(rare.Within[3].Probability-(1-math.Pow(1-p, 100))) > 1e-12 {
		t.Fatalf("百连概率 %v", rare.Within[3].Probability)
	}
	// 抽奖次数 6% × 2 次
	if math.Abs(a.RedrawsPerDraw-0.12) > 1e-9 {
		t.Fatalf("每抽再得 %v 次", a.RedrawsPerDraw)
	}
}

// 硬保底 + 伪随机：综合概率与蒙特卡洛一致；首达期望与稠密线性方程组 (I − Q)t = 1 的解一致。
func TestAnalyzeExactMatchesSimulationAndLinearSolve(t *testing.T) {
	pityID := int64(4)
	activity := Activity{PityThreshold: 6, PityPrizeID: &pityID}
	strategy := Strategy{Variance: VariancePRD}
	prizes := strategyPrizes()
	for i := range prizes {
		prizes[i].Quantity = -1
		if prizes[i].Winning() {
			prizes[i].Weight /= 4
		}
	}
	state := DrawState{Now: time.Now()}
	a := AnalyzeExact(activity, strategy, prizes, state)
	streak := AnalyzeStreak(activity, strategy, prizes, state)
	if math.Abs(a.WinRate-streak.LongRunWinRate) > 1e-5 || math.Abs(a.CycleMean-streak.Mean) > 1e-5 {
		t.Fatalf("与首中分析不一致：%v / %v", a.WinRate, streak.LongRunWinRate)
	}
	if a.PityShare <= 0 || a.Prizes[3].LongRun <= a.Prizes[3].Nominal {
		t.Fatalf("保底奖的综合概率应高于公示：%+v，保底占比 %v", a.Prizes[3], a.PityShare)
	}
	// 每人抽得足够长：保底发生在周期的末尾，抽得短会把最后一个没走完的周期截掉，低估保底奖
	sim := Simulate(activity, strategy, prizes, SimulationInput{Users: 300, DrawsPerUser: 2000, Seed: 11, IsolatedStock: true})
	for index, prize := range a.Prizes {
		if math.Abs(sim.Prizes[index].Rate-prize.LongRun) > 0.004 {
			t.Fatalf("%d 号：精确 %v，模拟 %v", prize.PrizeID, prize.LongRun, sim.Prizes[index].Rate)
		}
	}
	if math.Abs(sim.ValuePerDraw-a.ValuePerDraw)/a.ValuePerDraw > 0.02 {
		t.Fatalf("每抽价值：精确 %v，模拟 %v", a.ValuePerDraw, sim.ValuePerDraw)
	}

	// 稠密解：t = (I − Q)⁻¹ 1，Q 是「没抽中目标」的转移
	chain := buildChain(activity, strategy, prizes, state)
	n := len(chain.rows)
	for target, prize := range prizes {
		if a.Prizes[target].Never {
			continue
		}
		_, reset, advance := chain.hitSteps(target, prize.Winning())
		system := mat.NewDense(n, n, nil)
		for k := range n {
			system.Set(k, k, 1)
			system.Set(k, 0, system.At(k, 0)-reset[k])
			next := min(k+1, n-1)
			system.Set(k, next, system.At(k, next)-advance[k])
		}
		ones := mat.NewVecDense(n, nil)
		for k := range n {
			ones.SetVec(k, 1)
		}
		var solution mat.VecDense
		if err := solution.SolveVec(system, ones); err != nil {
			t.Fatal(err)
		}
		if got, want := a.Prizes[target].HitMean, solution.AtVec(0); math.Abs(got-want)/want > 1e-9 {
			t.Fatalf("%d 号首达期望：递推 %v，线性方程组 %v", prize.ID, got, want)
		}
	}
}

func TestAnalyzeExactNeverAndPityOnly(t *testing.T) {
	prizes := strategyPrizes()
	prizes[7].Weight = 0 // 不参与常规抽取，也不是保底奖
	pityID := int64(6)
	prizes[5].Weight = 0 // 只作保底奖
	prizes[5].Quantity = -1
	a := AnalyzeExact(Activity{PityThreshold: 4, PityPrizeID: &pityID}, Strategy{}, prizes, DrawState{Now: time.Now()})
	if !a.Prizes[7].Never || a.Prizes[7].LongRun != 0 {
		t.Fatalf("权重为 0 的奖品出不了：%+v", a.Prizes[7])
	}
	pity := a.Prizes[5]
	if pity.Never || pity.Nominal != 0 || pity.LongRun <= 0 || math.Abs(pity.LongRun-a.PityShare) > 1e-6 {
		t.Fatalf("纯保底奖只靠保底出：%+v，保底占比 %v", pity, a.PityShare)
	}
	if pity.HitMean < 4 || pity.Within[0].Probability != 0 {
		t.Fatalf("保底奖至少要 4 抽：%+v", pity)
	}
	// 全是谢谢参与：永远不中
	for i := range prizes {
		if prizes[i].Winning() {
			prizes[i].Weight = 0
		}
	}
	never := AnalyzeExact(Activity{}, Strategy{}, prizes, DrawState{Now: time.Now()})
	if never.WinRate != 0 || never.CycleMean != 0 {
		t.Fatalf("没有可中的奖品：%+v", never)
	}
}

// 每日运气：两端夹住平均，平均是对运气分布的积分（与大量采样的平均一致）。
func TestAnalyzeExactFortune(t *testing.T) {
	strategy := Strategy{Fortune: &Fortune{Spread: 0.3}}
	board := wheelPrizes()
	state := DrawState{Now: time.Now()}
	if AnalyzeExactFortune(Activity{}, Strategy{}, board, state, nil) != nil {
		t.Fatal("没开每日运气时没有这一项")
	}
	f := AnalyzeExactFortune(Activity{}, strategy, board, state, nil)
	if !(f.Low.Factor < 1 && f.High.Factor > 1 && f.Low.RealRate < f.Mean.RealRate && f.Mean.RealRate < f.High.RealRate) {
		t.Fatalf("两端应夹住平均：%+v", f)
	}
	var sum float64
	for _, p := range f.Mean.Prizes {
		sum += p
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatalf("平均综合概率之和 %v", sum)
	}
	// 采样对照：20 万个哈希的运气系数下真实奖品率的平均
	var sampled, factor float64
	const n = 200_000
	for i := range n {
		value := fortuneFactor(0.3, uint64(i)*0x9e3779b97f4a7c15+12345)
		factor += value
		sampled += exactBand(Activity{}, strategy, board, state, value).RealRate
	}
	if math.Abs(sampled/n-f.Mean.RealRate) > 5e-4 || math.Abs(factor/n-f.Mean.Factor) > 2e-3 {
		t.Fatalf("积分 %v / %v，采样 %v / %v", f.Mean.RealRate, f.Mean.Factor, sampled/n, factor/n)
	}
	// 至尊加成：保底运气 1.2 时，最倒霉的那天也按 1.2 算
	lifted := AnalyzeExactFortune(Activity{}, strategy, board, state, []SupremeGrade{{Boost: 1.5, Floor: 1.2}})
	if lifted.Low.Factor < 1.2-1e-9 || lifted.Mean.RealRate <= f.Mean.RealRate {
		t.Fatalf("至尊加成之后：%+v", lifted.Low)
	}
}
