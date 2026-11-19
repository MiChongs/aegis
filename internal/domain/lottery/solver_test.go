package lottery

import (
	"math"
	"strings"
	"testing"
	"time"
)

func solveModel(prizes []Prize) SolveModel {
	return SolveModel{Prizes: prizes, State: DrawState{Now: time.Now()}}
}

func checkOf(t *testing.T, result SolveResult, key string) SolveCheck {
	t.Helper()
	for _, check := range result.Checks {
		if check.Key == key {
			return check
		}
	}
	t.Fatalf("结果里没有目标 %s：%+v", key, result.Checks)
	return SolveCheck{}
}

// 只定综合中奖率：中奖格、未中格各自整体缩放，组内比例不变（KL 最小的解）。
func TestSolveWheelWinRate(t *testing.T) {
	prizes := strategyPrizes()
	for i := range prizes {
		prizes[i].Quantity = -1
	}
	result := SolveWheel(solveModel(prizes), SolveGoal{WinRate: 0.30})
	if !result.Feasible || !result.Converged {
		t.Fatalf("应可解：%+v", result)
	}
	if math.Abs(result.After.WinRate-0.30) > 1e-4 || math.Abs(result.Before.WinRate-0.44) > 1e-9 {
		t.Fatalf("中奖率 %v → %v", result.Before.WinRate, result.After.WinRate)
	}
	// 30 : 10 的两格仍是 3 : 1
	if ratio := float64(result.Prizes[1].NewWeight) / float64(result.Prizes[3].NewWeight); math.Abs(ratio-3) > 0.01 {
		t.Fatalf("组内比例变了：%v", ratio)
	}
	total := 0
	for _, prize := range result.Prizes {
		total += prize.NewWeight
	}
	if total != result.Total || result.Total < 100_000 {
		t.Fatalf("权重合计 %d / %d", total, result.Total)
	}
	if !checkOf(t, result, "winRate").Met || result.Divergence <= 0 {
		t.Fatalf("目标应达成且有改动：%+v", result.Checks)
	}
}

// 有硬保底时，综合中奖率不是权重占比：外层要把它校准回来。
func TestSolveWheelCalibratesThroughPity(t *testing.T) {
	prizes := strategyPrizes()
	for i := range prizes {
		prizes[i].Quantity = -1
	}
	pityID := int64(4)
	model := solveModel(prizes)
	model.Activity = Activity{PityThreshold: 5, PityPrizeID: &pityID}
	model.Strategy = Strategy{SoftPity: &SoftPity{Start: 2, Step: 5}}
	result := SolveWheel(model, SolveGoal{WinRate: 0.40})
	if !result.Feasible || !result.Converged || math.Abs(result.After.WinRate-0.40) > 2e-4 {
		t.Fatalf("综合中奖率应为 40%%：%+v", result.After)
	}
	// 综合 40% 时，权重占比上的中奖率明显低于 40%（保底与软保底在往上抬）
	var nominal float64
	for index, prize := range prizes {
		if prize.Winning() {
			nominal += result.Prizes[index].NewNominal
		}
	}
	if nominal > 0.37 {
		t.Fatalf("占比上的中奖率 %v 应明显低于综合值", nominal)
	}
	// 用解出来的权重独立复算一遍
	solved := strategyPrizes()
	for i := range solved {
		solved[i].Quantity = -1
		solved[i].Weight = result.Prizes[i].NewWeight
	}
	if again := AnalyzeExact(model.Activity, model.Strategy, solved, model.State); math.Abs(again.WinRate-0.40) > 2e-4 {
		t.Fatalf("复算 %v", again.WinRate)
	}
}

// 多个目标一起：定一格的综合概率、锁一格的占比、压每抽价值，其余按比例让路。
func TestSolveWheelCombinedGoals(t *testing.T) {
	prizes := wheelPrizes()
	before := AnalyzeExact(Activity{}, Strategy{}, prizes, DrawState{Now: time.Now()})
	goal := SolveGoal{
		RealRate: 0.25,
		Value:    before.ValuePerDraw * 0.8,
		Prizes: []SolvePrizeGoal{
			{Position: 1, Mode: SolveTarget, Probability: 0.00005},
			{Position: 5, Mode: SolveLock},
			{Position: 2, Mode: SolveRange, Min: 0.03},
		},
	}
	if err := ValidateSolveGoal(goal, prizes); err != nil {
		t.Fatal(err)
	}
	result := SolveWheel(solveModel(prizes), goal)
	if !result.Feasible || !result.Converged {
		t.Fatalf("应可解：%+v", result)
	}
	for _, check := range result.Checks {
		if !check.Met {
			t.Fatalf("目标未达成：%+v", check)
		}
	}
	if math.Abs(result.After.RealRate-0.25) > 2e-4 || result.After.ValuePerDraw > goal.Value*1.006 {
		t.Fatalf("真实奖品率 %v，每抽价值 %v（上限 %v）", result.After.RealRate, result.After.ValuePerDraw, goal.Value)
	}
	if got := result.Prizes[1].NewLongRun; math.Abs(got-0.00005)/0.00005 > 0.02 {
		t.Fatalf("永久会员综合概率 %v", got)
	}
	if got := result.Prizes[5].NewNominal; math.Abs(got-0.006) > 2e-5 {
		t.Fatalf("锁定的那一格占比变了：%v", got)
	}
	if result.Prizes[2].NewLongRun < 0.03-1e-4 {
		t.Fatalf("月度会员低于下限：%v", result.Prizes[2].NewLongRun)
	}
	if lower := checkOf(t, result, "prizeMin"); lower.Target != 0.03 || lower.After < 0.03-1e-4 {
		t.Fatalf("下限应按正数显示：%+v", lower)
	}
}

// 目标提在「求解对象眼里」：中奖加成 ×1.5、运气 ×1.7 之后的综合值达到目标，基础权重相应压低。
func TestSolveWheelThroughPersonalization(t *testing.T) {
	prizes := wheelPrizes()
	effects := PerkEffects{Names: []string{"永久会员"}, WinBoost: 1.5}
	model := solveModel(prizes)
	model.Personalize = func(activity Activity, board []Prize) (Activity, []Prize) {
		return activity, FortunePrizes(effects.BoostPrizes(board), 1.7)
	}
	result := SolveWheel(model, SolveGoal{RealRate: 0.30, PointsReturn: 14})
	if !result.Feasible || !result.Converged {
		t.Fatalf("应可解：%+v", result)
	}
	if math.Abs(result.After.RealRate-0.30) > 3e-4 {
		t.Fatalf("加成之后的真实奖品率应为 30%%：%v", result.After.RealRate)
	}
	if result.After.PointsReturn > 14*1.006 {
		t.Fatalf("积分回报 %v 超过上限", result.After.PointsReturn)
	}
	// 基础奖盘上的真实奖品占比要远低于 30%，乘上 2.55 倍才到 30%
	var base float64
	for index, prize := range prizes {
		if prize.Winning() && !prize.Fallback {
			base += result.Prizes[index].NewNominal
		}
	}
	if base > 0.16 || base < 0.12 {
		t.Fatalf("基础奖盘的真实奖品占比 %v", base)
	}
}

// 做不到的目标：保底 3 次必中，综合中奖率不可能低于 1/3。
func TestSolveWheelInfeasible(t *testing.T) {
	prizes := strategyPrizes()
	for i := range prizes {
		prizes[i].Quantity = -1
	}
	pityID := int64(2)
	model := solveModel(prizes)
	model.Activity = Activity{PityThreshold: 3, PityPrizeID: &pityID}
	result := SolveWheel(model, SolveGoal{WinRate: 0.05})
	if result.Feasible || checkOf(t, result, "winRate").Met {
		t.Fatalf("应判为做不到：%+v", result)
	}
	if result.After.WinRate < 1.0/3-1e-6 || len(result.Notes) == 0 {
		t.Fatalf("尽力而为的结果也不会低于 1/3：%v，%v", result.After.WinRate, result.Notes)
	}
	// 互相矛盾：两格的目标概率加起来超过 100%
	contradict := SolveWheel(solveModel(prizes), SolveGoal{Prizes: []SolvePrizeGoal{
		{Position: 1, Mode: SolveTarget, Probability: 0.7}, {Position: 3, Mode: SolveTarget, Probability: 0.6},
	}})
	if contradict.Feasible {
		t.Fatalf("矛盾的目标应判为做不到：%+v", contradict.Checks)
	}
}

// 零权重的奖品定了目标概率：给它一个起点，解得出来。
func TestSolveWheelSeedsZeroWeightPrize(t *testing.T) {
	prizes := wheelPrizes()
	prizes[1].Weight = 0
	result := SolveWheel(solveModel(prizes), SolveGoal{Prizes: []SolvePrizeGoal{{Position: 1, Mode: SolveTarget, Probability: 0.0002}}})
	if !result.Feasible || result.Prizes[1].NewWeight == 0 {
		t.Fatalf("应分到权重：%+v", result.Prizes[1])
	}
	if math.Abs(result.Prizes[1].NewLongRun-0.0002)/0.0002 > 0.01 {
		t.Fatalf("综合概率 %v", result.Prizes[1].NewLongRun)
	}
	if len(result.Notes) == 0 || !strings.Contains(result.Notes[0], "永久会员") {
		t.Fatalf("应说明给了它权重：%v", result.Notes)
	}
	// 没有任何目标：权重不变
	same := SolveWheel(solveModel(wheelPrizes()), SolveGoal{})
	for index, prize := range same.Prizes {
		if prize.NewWeight != wheelPrizes()[index].Weight {
			t.Fatalf("没有目标时权重应不变：%+v", prize)
		}
	}
}

func TestIntegerWeights(t *testing.T) {
	weights := integerWeights([]float64{0.5, 0.3, 0.2 - 1e-7, 1e-7, 0}, 1000)
	sum := 0
	for _, w := range weights {
		sum += w
	}
	if sum != 1000 || weights[3] != 1 || weights[4] != 0 || weights[0] < 499 {
		t.Fatalf("整数化不对：%v", weights)
	}
	if total := solveTotal(0, wheelPrizes(), []float64{0.99999, 0.00001}); total != 1_000_000 {
		t.Fatalf("十万分之一的概率要放大合计，得到 %d", total)
	}
	if total := solveTotal(5000, wheelPrizes(), nil); total != 5000 {
		t.Fatalf("指定了合计就用指定的，得到 %d", total)
	}
}

func TestValidateSolveGoal(t *testing.T) {
	grid, wheel := strategyPrizes(), wheelPrizes()
	cases := []struct {
		goal   SolveGoal
		prizes []Prize
		want   string
	}{
		{SolveGoal{WinRate: 1.2}, grid, "综合中奖率"},
		{SolveGoal{WinRate: 0.3}, wheel, "真实奖品率"},
		{SolveGoal{Value: -1}, grid, "不能为负"},
		{SolveGoal{ValueMode: "about"}, grid, "方式"},
		{SolveGoal{Total: 5}, grid, "合计"},
		{SolveGoal{Prizes: []SolvePrizeGoal{{Position: 99, Mode: SolveLock}}}, grid, "不在奖盘上"},
		{SolveGoal{Prizes: []SolvePrizeGoal{{Position: 1, Mode: SolveLock}, {Position: 1, Mode: SolveLock}}}, grid, "不止一个"},
		{SolveGoal{Prizes: []SolvePrizeGoal{{Position: 1, Mode: SolveTarget}}}, grid, "目标概率"},
		{SolveGoal{Prizes: []SolvePrizeGoal{{Position: 1, Mode: SolveRange}}}, grid, "至少要填一端"},
		{SolveGoal{Prizes: []SolvePrizeGoal{{Position: 1, Mode: SolveRange, Min: 0.5, Max: 0.2}}}, grid, "不能高于"},
		{SolveGoal{Prizes: []SolvePrizeGoal{{Position: 1, Mode: "pin"}}}, grid, "不认识"},
	}
	for _, c := range cases {
		err := ValidateSolveGoal(c.goal, c.prizes)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%+v 应报「%s」，得到 %v", c.goal, c.want, err)
		}
	}
	if err := ValidateSolveGoal(SolveGoal{WinRate: 0.3, Value: 5, Prizes: []SolvePrizeGoal{{Position: 1, Mode: SolveRange, Max: 0.4}}}, grid); err != nil {
		t.Fatal(err)
	}
}

// 等式约束的 I-投影：解落在约束上，且是 KL 最小的那个（与闭式解对照）。
func TestIProjectEquality(t *testing.T) {
	q := []float64{0.5, 0.3, 0.2}
	// p₁ = 0.2：其余两格按原比例 3 : 2 分 0.8
	p, feasible := iProject(q, []linearConstraint{{a: []float64{1, 0, 0}, b: 0.2, eq: true}})
	if !feasible || math.Abs(p[0]-0.2) > 1e-8 || math.Abs(p[1]-0.48) > 1e-8 || math.Abs(p[2]-0.32) > 1e-8 {
		t.Fatalf("等式投影 %v", p)
	}
	// 等式往上调（不等式做不到的方向）
	p, feasible = iProject(q, []linearConstraint{{a: []float64{0, 0, 1}, b: 0.6, eq: true}})
	if !feasible || math.Abs(p[2]-0.6) > 1e-8 || math.Abs(p[0]/p[1]-5.0/3) > 1e-6 {
		t.Fatalf("等式投影 %v", p)
	}
	// 两条矛盾的等式
	if _, feasible = iProject(q, []linearConstraint{
		{a: []float64{1, 0, 0}, b: 0.7, eq: true}, {a: []float64{0, 1, 0}, b: 0.6, eq: true},
	}); feasible {
		t.Fatal("矛盾的等式应不可行")
	}
}
