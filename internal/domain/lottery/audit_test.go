package lottery

import (
	"encoding/json"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
	"time"
)

// auditSample 用真实的方案抽 n 次，cheat 为真时把每 10 次里的一次换成第一个中奖奖品。
func auditSample(n int, strategy Strategy, cheat bool, seed uint64) []AuditDraw {
	src := rand.NewPCG(seed, seed+1)
	rng := rand.New(src)
	draws := make([]AuditDraw, 0, n)
	prizes := strategyPrizes()
	for t := range n {
		// 每次的状态不同（软保底、运气），方案也不同
		state := DrawState{Now: time.Now(), MissStreak: rng.IntN(6), WinsTotal: rng.IntN(30), LuckExpected: 15, LuckVariance: 8}
		plan := PlanDraw(Activity{}, strategy, prizes, state)
		index, _ := Sample(plan, src)
		picked := prizes[index].ID
		if cheat && t%10 == 0 {
			picked = prizes[1].ID
		}
		draws = append(draws, AuditDraw{Odds: plan.Odds, Picked: picked})
	}
	return draws
}

func TestAuditPassesHonestDraws(t *testing.T) {
	strategy := Strategy{SoftPity: &SoftPity{Start: 2, Step: 10}, Luck: &LuckBalance{Strength: 0.4}}
	failures := 0
	for seed := range uint64(20) {
		if !Audit(auditSample(5000, strategy, false, seed+1)).Passed {
			failures++
		}
	}
	// α = 0.01、整体加边际两道检验：20 次里最多偶尔误报一两次
	if failures > 2 {
		t.Fatalf("诚实的出奖被判不通过 %d/20 次", failures)
	}
	result := Audit(auditSample(5000, strategy, false, 99))
	if result.DF != 7 || result.Draws != 5000 {
		t.Fatalf("自由度应为类别数 − 1：%+v", result)
	}
}

func TestAuditCatchesBias(t *testing.T) {
	result := Audit(auditSample(5000, Strategy{SoftPity: &SoftPity{Start: 2, Step: 10}}, true, 7))
	if result.Passed || result.PValue > 1e-6 {
		t.Fatalf("偏离方案 10%% 的出奖应被识破：p=%v", result.PValue)
	}
	if !result.Prizes[1].Flagged || result.Prizes[1].Z <= 0 {
		t.Fatalf("被多发的奖品应被标出：%+v", result.Prizes[1])
	}
}

func TestAuditSkipsPityAndFallback(t *testing.T) {
	draws := auditSample(100, Strategy{}, false, 1)
	draws[0].Pity, draws[1].Fallback = true, true
	if r := Audit(draws); r.Draws != 98 || r.Skipped != 2 {
		t.Fatalf("保底与兜底不计入：%+v", r)
	}
}

// seededDraws 像线上那样抽：每次一个新种子，方案连同种子过一遍 JSON（落库再读回来）。
func seededDraws(t *testing.T, n int) []AuditDraw {
	t.Helper()
	rng := rand.New(rand.NewPCG(5, 6))
	prizes := strategyPrizes()
	strategy := Strategy{SoftPity: &SoftPity{Start: 2, Step: 7}, Luck: &LuckBalance{Strength: 0.37},
		Fortune: &Fortune{Spread: 0.3}}
	draws := make([]AuditDraw, 0, n)
	for i := range n {
		state := DrawState{Now: time.Now(), MissStreak: rng.IntN(8), WinsTotal: rng.IntN(30),
			LuckExpected: 15 * rng.Float64(), LuckVariance: 1 + 8*rng.Float64()}
		// 运气系数让权重带上乱七八糟的小数：复算必须在这种权重上也逐位一致
		plan := PlanDraw(Activity{}, strategy, FortunePrizes(prizes, 0.5+rng.Float64()), state)
		src, seed, err := NewSeededDrawSource()
		if err != nil {
			t.Fatal(err)
		}
		index, ok := Sample(plan, src)
		if !ok {
			t.Fatal("应抽得到")
		}
		plan.Seed, plan.Picked = seed, prizes[index].ID
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		var stored DrawPlan
		if err := json.Unmarshal(raw, &stored); err != nil {
			t.Fatal(err)
		}
		draws = append(draws, AuditDraw{ID: int64(i + 1), DrawNo: "LTD" + strconv.Itoa(i), Odds: stored.Odds,
			Picked: stored.Picked, Seed: stored.Seed})
	}
	return draws
}

// 逐笔复算：方案与种子落库再读回来，重抽的结果与记录逐笔一致。
func TestReplayDrawsMatchesRecordedDraws(t *testing.T) {
	draws := seededDraws(t, 3000)
	replay := ReplayDraws(draws)
	if replay.Checked != 3000 || replay.Matched != 3000 || len(replay.Mismatches) != 0 {
		t.Fatalf("诚实的记录应全部复算一致：%+v", replay)
	}
	if !Audit(draws).Passed {
		t.Fatal("全部一致时审计应通过")
	}
}

// 只动一条记录，统计检验看不出来，逐笔复算能指到那一条。
func TestReplayDrawsCatchesSingleTamperedDraw(t *testing.T) {
	draws := seededDraws(t, 3000)
	victim := &draws[1234]
	original := victim.Picked
	for _, odds := range victim.Odds {
		if odds.PrizeID != original && odds.Weight > 0 {
			victim.Picked = odds.PrizeID // 把结果改成另一个奖品
			break
		}
	}
	result := Audit(draws)
	if auditStatistics(draws).Passed == false {
		t.Fatal("三千次里改一次，统计检验不该报警（否则这个用例没有说明问题）")
	}
	if result.Passed || result.Replay.Matched != 2999 || len(result.Replay.Mismatches) != 1 {
		t.Fatalf("应指出被改的那一条：%+v", result.Replay)
	}
	if m := result.Replay.Mismatches[0]; m.DrawID != 1235 || m.Recorded != victim.Picked || m.Replayed != original {
		t.Fatalf("对不上的记录：%+v", m)
	}

	// 改权重（方案被动过）同样对不上；种子坏了也算对不上
	draws = seededDraws(t, 200)
	draws[7].Seed = "zz"
	draws[9].Seed = draws[10].Seed
	replay := ReplayDraws(draws)
	if replay.Checked != 200 || replay.Matched > 199 || replay.Mismatches[0].DrawID != 8 || replay.Mismatches[0].Replayed != 0 {
		t.Fatalf("坏种子应算对不上：%+v", replay)
	}
}

func TestReplayDrawsSkipsUnsampledDraws(t *testing.T) {
	draws := seededDraws(t, 100)
	draws[0].Pity = true                                      // 硬保底：没有抽样
	draws[1].Fallback = true                                  // 权重全为 0 改发兜底：没有抽样
	draws[2].Seed = ""                                        // 上线之前的记录
	draws[3].Fallback, draws[3].Drawn = true, draws[3].Picked // 抽到的被抢光，改发了兜底
	draws[3].Picked = 1
	replay := ReplayDraws(draws)
	if replay.Skipped != 2 || replay.Unseeded != 1 || replay.Checked != 97 || replay.Matched != 97 {
		t.Fatalf("没有抽样的不复算，被抢光的核对抽样本身：%+v", replay)
	}
	if _, ok := ReplaySample(DrawPlan{Seed: strings.Repeat("0", 64)}); ok {
		t.Fatal("没有权重时复算不出结果")
	}
}
