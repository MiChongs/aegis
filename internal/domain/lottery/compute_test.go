package lottery

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func computeDay() time.Time {
	return time.Date(2026, 11, 20, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
}

func gridUnlimited() []Prize {
	prizes := strategyPrizes()
	for i := range prizes {
		prizes[i].Quantity = -1
	}
	return prizes
}

func onePersona(activity Activity, prizes []Prize) []ComputePersona {
	return []ComputePersona{{Key: "user", Name: "普通用户", Share: 1, Activity: activity, Prizes: prizes}}
}

// 同一个种子：1 个与 8 个工作协程得到逐字节相同的结果。
func TestComputeIsDeterministic(t *testing.T) {
	activity := Activity{Strategy: Strategy{Fortune: &Fortune{Spread: 0.3}, SoftPity: &SoftPity{Start: 3, Step: 5}}}
	scenario := ComputeScenario{
		Personas: onePersona(activity, gridUnlimited()), Users: 200, Days: 3, DrawsPerDay: 8, Replications: 12,
		Seed: 20261120, Start: computeDay(),
	}
	run := func(workers int) string {
		s := scenario
		s.Workers = workers
		result, err := Compute(context.Background(), s, nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(result)
		return string(raw)
	}
	if a, b := run(1), run(8); a != b {
		t.Fatal("同一个种子在不同并行度下结果不同")
	}
	other := scenario
	other.Seed++
	result, _ := Compute(context.Background(), other, nil)
	raw, _ := json.Marshal(result)
	if string(raw) == run(4) {
		t.Fatal("换了种子结果应不同")
	}
	if result.Seed != "20261121" || result.Draws != 200*3*8*12 || result.Fortune == nil {
		t.Fatalf("规模与种子：%+v", result)
	}
	if math.Abs(result.Fortune.Mean-1) > 0.03 {
		t.Fatalf("运气系数的平均应约为 1：%v", result.Fortune.Mean)
	}
}

// 推演与精确分析互相印证：硬保底 + 伪随机下，综合概率落在推演的置信区间附近。
func TestComputeAgreesWithExactAnalysis(t *testing.T) {
	pityID := int64(4)
	activity := Activity{PityThreshold: 6, PityPrizeID: &pityID, Strategy: Strategy{Variance: VariancePRD}}
	prizes := gridUnlimited()
	for i := range prizes {
		if prizes[i].Winning() {
			prizes[i].Weight /= 4
		}
	}
	exact := AnalyzeExact(activity, activity.Strategy, prizes, DrawState{Now: computeDay()})
	result, err := Compute(context.Background(), ComputeScenario{
		Personas: onePersona(activity, prizes), Users: 60, Days: 20, DrawsPerDay: 100, Replications: 24,
		Seed: 7, Start: computeDay(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	within := func(name string, estimate float64, stderr float64, want float64) {
		t.Helper()
		if math.Abs(estimate-want) > 4*stderr+0.002 {
			t.Fatalf("%s：推演 %v（标准误 %v），精确 %v", name, estimate, stderr, want)
		}
	}
	within("中奖率", result.WinRate.Mean, result.WinRate.StdErr, exact.WinRate)
	within("保底占比", result.PityShare.Mean, result.PityShare.StdErr, exact.PityShare)
	for index, prize := range result.Prizes {
		within(prize.Name, prize.Rate.Mean, prize.Rate.StdErr, exact.Prizes[index].LongRun)
		if !(prize.Low <= prize.Rate.Mean+1e-9 && prize.Rate.Mean <= prize.High+1e-9) {
			t.Fatalf("Wilson 区间应盖住合并比例：%+v", prize)
		}
	}
	if math.Abs(result.ValuePerDraw.Mean-exact.ValuePerDraw)/exact.ValuePerDraw > 0.02 {
		t.Fatalf("每抽价值：推演 %v，精确 %v", result.ValuePerDraw.Mean, exact.ValuePerDraw)
	}
	if !(result.WinRate.Low < result.WinRate.Mean && result.WinRate.Mean < result.WinRate.High) || result.WinRate.N != 24 {
		t.Fatalf("应给出置信区间：%+v", result.WinRate)
	}
}

// 全站共享的库存与每日限量：恰好出到上限，给出抽空的时间与触顶的时刻。
func TestComputeSharedStockAndDailyCap(t *testing.T) {
	prizes := gridUnlimited()
	prizes[3].Quantity, prizes[3].Used = 60, 10 // 10% 的 5 积分：还剩 50 个
	prizes[1].DailyLimit = 40                   // 30% 的 2 积分：全站每天 40 个
	const worlds, days = 6, 4
	result, err := Compute(context.Background(), ComputeScenario{
		Personas: onePersona(Activity{}, prizes), Users: 300, Days: days, DrawsPerDay: 3, Replications: worlds,
		Seed: 3, Start: computeDay(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	limited, capped := result.Prizes[3], result.Prizes[1]
	if limited.Wins != 50*worlds || limited.StockOut == nil || limited.StockOut.Probability != 1 {
		t.Fatalf("限量奖品每个世界恰好出 50 个：%+v", limited)
	}
	if day := limited.StockOut.Day.Mean; day < 1 || day > 2 {
		t.Fatalf("900 抽 × 10%% 一天就该抽空：第 %v 天", day)
	}
	if capped.Wins != 40*days*worlds || capped.DailyCap == nil || capped.DailyCap.Share != 1 || capped.DailyCap.At >= 1 {
		t.Fatalf("每日限量每天恰好出 40 个：%+v", capped)
	}
	if capped.Blocked == 0 || limited.Blocked == 0 {
		t.Fatal("触顶之后应记为受限")
	}
	if len(result.Stock) != 1 || result.Stock[0].Quantity != 60 {
		t.Fatalf("库存曲线：%+v", result.Stock)
	}
	series := result.Stock[0].Used
	if series[0] < 10 || series[days-1] != 60 {
		t.Fatalf("库存曲线应从已出的 10 个走到 60：%v", series)
	}
	for day := 1; day < days; day++ {
		if series[day] < series[day-1] {
			t.Fatalf("库存曲线应单调：%v", series)
		}
	}
	if len(result.Daily) != days || result.Daily[0].Draws != 900 {
		t.Fatalf("每天的抽奖次数：%+v", result.Daily)
	}
}

// 几种身份的人混在一起抽：各自的次数、各自的概率、各自的积分回报。
func TestComputePersonasAndArbitrage(t *testing.T) {
	base := wheelPrizes()
	boosted := PerkEffects{Names: []string{"永久会员"}, WinBoost: 1.5}.BoostPrizes(base)
	// 永久会员抽中会员类奖品折成积分：到账表与奖品本身不同
	payout := make([]float64, len(base))
	redraws := make([]float64, len(base))
	for i, prize := range base {
		switch prize.Kind {
		case KindPoints:
			payout[i] = float64(prize.Reward.Amount)
		case KindVipDays:
			payout[i] = float64(prize.Reward.Amount) * 10
		case KindLotteryDraws:
			redraws[i] = float64(prize.Reward.Amount)
		}
	}
	activity := Activity{}
	scenario := ComputeScenario{
		Personas: []ComputePersona{
			{Key: "user", Name: "普通用户", Share: 3, Activity: activity, Prizes: base},
			{Key: "lifetime", Name: "永久会员", Share: 1, Activity: activity, Prizes: boosted, Payout: payout, Redraws: redraws, DrawsPerDay: 10},
		},
		Users: 400, Days: 2, DrawsPerDay: 4, Replications: 16, Seed: 9, Start: computeDay(), Price: 18,
	}
	if got := scenario.TotalDraws(); got != (300*4+100*10)*2*16 {
		t.Fatalf("总次数 %d", got)
	}
	result, err := Compute(context.Background(), scenario, nil)
	if err != nil {
		t.Fatal(err)
	}
	user, lifetime := result.Personas[0], result.Personas[1]
	if user.Users != 300 || lifetime.Users != 100 || user.Draws != 300*4*2*16 || lifetime.Draws != 100*10*2*16 {
		t.Fatalf("人数与次数：%+v / %+v", user, lifetime)
	}
	if lifetime.RealRate.Mean <= user.RealRate.Mean*1.2 {
		t.Fatalf("中奖加成的人真实奖品率应明显更高：%v / %v", lifetime.RealRate.Mean, user.RealRate.Mean)
	}
	// 普通用户：10×70% + 50×3% + 288×0.6% ≈ 10.2 积分，再乘 1/(1−0.12)，低于 18；
	// 永久会员多了会员折算（月度 300、15 天 150），远超 18
	if user.Arbitrage || user.Return.Mean > 15 {
		t.Fatalf("普通用户不该稳赚：%+v", user.Return)
	}
	if !lifetime.Arbitrage || lifetime.Return.Low <= 18 {
		t.Fatalf("永久会员的折算应让积分买次数稳赚：%+v", lifetime.Return)
	}
	if result.Arbitrage == nil || result.Arbitrage.Price != 18 || result.Arbitrage.Probability < 0 {
		t.Fatalf("全体的套利检查：%+v", result.Arbitrage)
	}
	if result.Budget != nil {
		t.Fatalf("没设积分预算时不该有预算一项：%+v", result.Budget)
	}
	if result.WinsPerUser.Max == 0 || len(result.WinsPerUser.Buckets) == 0 || result.ValuePerUser.P90 < result.ValuePerUser.P50 {
		t.Fatalf("每人分布：%+v / %+v", result.WinsPerUser, result.ValuePerUser)
	}
	if len(lifetime.Prizes) != len(base) || result.Prizes[1].BaseRate <= 0 {
		t.Fatalf("各身份的奖品分布：%+v", lifetime.Prizes)
	}
}

// 每日积分预算：一早用完之后积分格（连同积分兜底格）整天不出，剩下的概率全落到别的奖品上。
// 线上出过两次（2026-10-28）：预算被会员折算吃光，奖盘整天变了样。推演要能把它演出来。
func TestComputeBudgetExhaustion(t *testing.T) {
	activity := Activity{Strategy: Strategy{Budget: &Budget{DailyPoints: 3000}}}
	result, err := Compute(context.Background(), ComputeScenario{
		Personas: onePersona(activity, wheelPrizes()), Users: 500, Days: 2, DrawsPerDay: 4, Replications: 8,
		Seed: 5, Start: computeDay(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Budget == nil || result.Budget.DailyPoints != 3000 || result.Budget.Share != 1 || result.Budget.At <= 0 || result.Budget.At >= 1 {
		t.Fatalf("每天 2000 抽、预算 3000 积分，每天都该用完：%+v", result.Budget)
	}
	if floor := result.Prizes[0]; floor.Blocked == 0 || floor.Rate.Mean > floor.BaseRate/2 {
		t.Fatalf("预算用完之后积分格应出不了：%+v", floor)
	}
	// 公示的真实奖品率是 30%，预算用完后积分格清零，实际远高于公示
	if result.RealRate.Mean < 0.5 {
		t.Fatalf("预算用完后真实奖品率应被抬高：%v", result.RealRate.Mean)
	}
	if result.PointsPerWorld.Mean > 2*3000*1.2 {
		t.Fatalf("两天发出的积分不该明显超过预算：%v", result.PointsPerWorld.Mean)
	}
}

func TestComputeValidationAndCancel(t *testing.T) {
	ok := ComputeScenario{Personas: onePersona(Activity{}, gridUnlimited()), Users: 10, Days: 1, DrawsPerDay: 1, Replications: 1}
	mutate := func(change func(*ComputeScenario)) ComputeScenario {
		s := ok
		s.Personas = append([]ComputePersona(nil), ok.Personas...)
		change(&s)
		return s
	}
	cases := []struct {
		scenario ComputeScenario
		want     string
	}{
		{mutate(func(s *ComputeScenario) { s.Personas = nil }), "人群"},
		{mutate(func(s *ComputeScenario) { s.Users = 0 }), "人数"},
		{mutate(func(s *ComputeScenario) { s.Days = 99 }), "天数"},
		{mutate(func(s *ComputeScenario) { s.DrawsPerDay = 0 }), "抽奖次数"},
		{mutate(func(s *ComputeScenario) { s.Replications = 0 }), "平行世界"},
		{mutate(func(s *ComputeScenario) { s.Source = "gift" }), "次数来源"},
		{mutate(func(s *ComputeScenario) { s.Personas[0].Prizes = nil }), "空的"},
		{mutate(func(s *ComputeScenario) { s.Personas[0].Share = 0 }), "占比合计"},
		{mutate(func(s *ComputeScenario) { s.Personas[0].Payout = []float64{1} }), "到账表"},
		{mutate(func(s *ComputeScenario) {
			s.Personas = append(s.Personas, ComputePersona{Name: "会员", Share: 1, Prizes: gridUnlimited()[:3]})
		}), "对不上"},
		{mutate(func(s *ComputeScenario) { s.Users, s.Days, s.DrawsPerDay, s.Replications = 100_000, 60, 200, 256 }), "超过上限"},
	}
	for _, c := range cases {
		if err := ValidateCompute(c.scenario); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("应报「%s」，得到 %v", c.want, err)
		}
		if _, err := Compute(context.Background(), c.scenario, nil); err == nil {
			t.Fatalf("不合法的场景不应开始推演：%s", c.want)
		}
	}
	if err := ValidateCompute(ok); err != nil {
		t.Fatal(err)
	}

	// 取消：进度回调里取消，推演尽快停下
	ctx, cancel := context.WithCancel(context.Background())
	big := ok
	big.Users, big.Days, big.DrawsPerDay, big.Replications, big.Workers = 2000, 3, 20, 200, 2
	started := time.Now()
	_, err := Compute(ctx, big, func(done, total int) {
		if done == 2 {
			cancel()
		}
		if total != 200 {
			t.Errorf("总数 %d", total)
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应返回 context.Canceled，得到 %v", err)
	}
	if time.Since(started) > 20*time.Second {
		t.Fatalf("取消后没有尽快停下：%v", time.Since(started))
	}
}

func TestPersonaLayout(t *testing.T) {
	personas := []ComputePersona{{Share: 70}, {Share: 20}, {Share: 10}}
	if got := personaLayout(personas, 7); got[0]+got[1]+got[2] != 7 || got[0] != 5 {
		t.Fatalf("7 人按 7:2:1：%v", got)
	}
	if got := personaLayout([]ComputePersona{{Share: 0}, {Share: 0}}, 5); got[0]+got[1] != 5 {
		t.Fatalf("占比全为 0 时均分：%v", got)
	}
}

// BenchmarkCompute 一个接近线上的奖盘（每日运气 + 风控规则 + 软保底）每秒能演多少次抽奖。
func BenchmarkCompute(b *testing.B) {
	activity := Activity{Strategy: NormalizeStrategy(Strategy{
		Fortune:  &Fortune{Spread: 0.3},
		SoftPity: &SoftPity{Start: 5, Step: 5},
		Risk:     &RiskGuard{Rules: []RiskRule{{Signal: "deviceAccounts", Op: "gt", Threshold: 3, Action: RiskFloor}}},
	})}
	scenario := ComputeScenario{
		Personas: onePersona(activity, wheelPrizes()), Users: 2000, Days: 1, DrawsPerDay: 10, Replications: 48,
		Seed: 1, Start: computeDay(),
	}
	b.ResetTimer()
	for range b.N {
		result, err := Compute(context.Background(), scenario, nil)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(result.Draws)/b.Elapsed().Seconds()*float64(b.N), "draws/s")
	}
}
