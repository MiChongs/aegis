package lottery

import (
	"testing"
	"time"
)

// allWinPrizes 没有「谢谢参与」的奖盘：10 积分是兜底，线上活动就是这种（截图里的场景）。
func allWinPrizes() []Prize {
	return []Prize{
		{ID: 1, Position: 0, Kind: KindVipLifetime, Reward: Reward{Type: KindVipLifetime}, Weight: 1, Quantity: -1},
		{ID: 2, Position: 1, Kind: KindPoints, Reward: Reward{Type: KindPoints, Amount: 10}, Weight: 70000, Quantity: -1, Fallback: true},
		{ID: 3, Position: 2, Kind: KindVipDays, Reward: Reward{Type: KindVipDays, Amount: 30}, Weight: 2000, Quantity: -1},
		{ID: 4, Position: 3, Kind: KindPoints, Reward: Reward{Type: KindPoints, Amount: 50}, Weight: 27999, Quantity: -1},
	}
}

func TestRiskLegacyFieldsBecomeFloorRules(t *testing.T) {
	strategy := NormalizeStrategy(Strategy{Risk: &RiskGuard{DeviceAccounts: 3, IPAccounts: 5}})
	if strategy.Risk == nil || len(strategy.Risk.Rules) != 2 || strategy.Risk.DeviceAccounts != 0 {
		t.Fatalf("旧字段应转成两条规则：%+v", strategy.Risk)
	}
	if rule := strategy.Risk.Rules[0]; rule.Signal != RiskSignalDeviceAccounts || rule.Op != RiskGT || rule.Threshold != 3 || rule.Action != RiskFloor {
		t.Fatalf("同设备规则不对：%+v", rule)
	}
	if err := ValidateStrategy(strategy, 4); err != nil {
		t.Fatal(err)
	}
	// 没经过清洗的旧配置（已保存的活动）也照旧生效
	if rules := (&RiskGuard{DeviceAccounts: 3}).EffectiveRules(); len(rules) != 1 {
		t.Fatalf("未清洗的旧字段也要生效：%+v", rules)
	}
}

func TestRiskFloorKeepsFallbackAtFullProbability(t *testing.T) {
	strategy := NormalizeStrategy(Strategy{Risk: &RiskGuard{DeviceAccounts: 3}})
	plan := PlanDraw(Activity{}, strategy, allWinPrizes(), DrawState{Now: time.Now(), DeviceAccounts: 5, IPAccounts: 1})
	if !plan.Risk || plan.RiskReport.Action != RiskFloor || plan.FloorID != 2 {
		t.Fatalf("应只发兜底：risk=%v report=%+v floor=%d", plan.Risk, plan.RiskReport, plan.FloorID)
	}
	check := plan.RiskReport.Checks[0]
	if !check.Hit || check.Value != 5 || check.Threshold != 3 {
		t.Fatalf("逐条结果应记下取值与阈值：%+v", check)
	}
	for _, odds := range plan.Odds {
		switch odds.PrizeID {
		case 2:
			if !near(odds.Probability, 1) || odds.Blocked != "" {
				t.Fatalf("兜底奖应为 100%%：%+v", odds)
			}
		default:
			if odds.Blocked != FactorRisk || odds.Probability != 0 || odds.Factors[0].Note == "" {
				t.Fatalf("其余中奖奖品应停发并写明原因：%+v", odds)
			}
		}
	}
	if _, ok := Sample(plan, nil); !ok {
		// Sample 需要随机源；能走到这里说明权重不全为 0
		t.Fatal("兜底奖应可抽")
	}
}

func TestRiskDampRareAndNoPity(t *testing.T) {
	strategy := NormalizeStrategy(Strategy{Risk: &RiskGuard{Rules: []RiskRule{
		{Signal: RiskSignalAccountAge, Op: RiskLT, Threshold: 24, Action: RiskDamp, Factor: 0.5},
		{Signal: RiskSignalBurst, Op: RiskGT, Threshold: 10, Action: RiskRare, RareBelow: 0.05},
		{Signal: RiskSignalRiskScore, Op: RiskGTE, Threshold: 60, Action: RiskNoPity},
		{Signal: RiskSignalWinsToday, Op: RiskGT, Threshold: 100, Action: RiskDeny, Disabled: true},
	}}})
	if err := ValidateStrategy(strategy, 4); err != nil {
		t.Fatal(err)
	}
	state := DrawState{Now: time.Now(), RecentDraws: 20, Profile: BehaviorProfile{RegisteredDays: 0.5, RiskScore30d: 80}}
	plan := PlanDraw(Activity{}, strategy, allWinPrizes(), state)
	report := plan.RiskReport
	if report.Action != RiskRare || !report.NoPity || len(report.Checks) != 3 || plan.Risk {
		t.Fatalf("最重处置应为停发稀有、保底关闭、停用的规则不评估：%+v", report)
	}
	byID := map[int64]PrizeOdds{}
	for _, odds := range plan.Odds {
		byID[odds.PrizeID] = odds
	}
	// 公示 0.001% 的永久会员、2% 的月度会员低于 5%：停发；兜底不受影响；50 积分降权一半
	if byID[1].Blocked != FactorRisk || byID[3].Blocked != FactorRisk {
		t.Fatalf("稀有奖品应停发：%+v %+v", byID[1], byID[3])
	}
	if byID[2].Weight != 70000 {
		t.Fatalf("兜底奖不该降权：%+v", byID[2])
	}
	if byID[4].Weight != 27999*0.5 {
		t.Fatalf("50 积分应降权一半：%+v", byID[4])
	}
	pityID := int64(3)
	if _, ok := PityChoice(Activity{PityPrizeID: &pityID, PityThreshold: 1}, plan, allWinPrizes(), 99); ok {
		t.Fatal("不触发保底时硬保底不该生效")
	}
}

func TestRiskDenyAndMissingSignals(t *testing.T) {
	strategy := NormalizeStrategy(Strategy{Risk: &RiskGuard{Rules: []RiskRule{
		{Signal: RiskSignalDeviceAccounts, Op: RiskGT, Threshold: 1, Action: RiskFloor},
		{Signal: RiskSignalNoDevice, Op: RiskGTE, Threshold: 1, Action: RiskDeny},
	}}})
	plan := PlanDraw(Activity{}, strategy, allWinPrizes(), DrawState{Now: time.Now(), DeviceMissing: true, DeviceAccounts: 1})
	if !plan.RiskReport.Denied() || !plan.RiskReport.Checks[0].Missing || plan.RiskReport.Checks[0].Hit {
		t.Fatalf("没有设备号：同设备取不到、缺设备标识命中拒绝：%+v", plan.RiskReport)
	}
}

func TestRiskValidation(t *testing.T) {
	bad := []RiskRule{
		{Signal: "nope", Op: RiskGT, Threshold: 1, Action: RiskFloor},
		{Signal: RiskSignalBurst, Op: "eq", Threshold: 1, Action: RiskFloor},
		{Signal: RiskSignalBurst, Op: RiskGT, Threshold: 1, Action: RiskDamp, Factor: 1},
		{Signal: RiskSignalBurst, Op: RiskGT, Threshold: 1, Action: RiskRare},
		{Signal: RiskSignalLegacy, Op: RiskGT, Threshold: 1, Action: RiskFloor},
		{Signal: RiskSignalBurst, Op: RiskGT, Threshold: -1, Action: RiskFloor},
	}
	for _, rule := range bad {
		strategy := NormalizeStrategy(Strategy{Risk: &RiskGuard{Rules: []RiskRule{rule}}})
		if ValidateStrategy(strategy, 4) == nil {
			t.Fatalf("应拒绝：%+v", rule)
		}
	}
}

func TestSubnetOf(t *testing.T) {
	cases := map[string]string{
		"203.0.113.77":         "203.0.113.0/24",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"::ffff:198.51.100.9":  "198.51.100.0/24",
		"not-an-ip":            "",
		"":                     "",
	}
	for in, want := range cases {
		if got := SubnetOf(in); got != want {
			t.Fatalf("SubnetOf(%q) = %q，应为 %q", in, got, want)
		}
	}
}

func TestParseDrawSearch(t *testing.T) {
	terms := ParseDrawSearch(`LTD202611020015249524663899 user:张三 prize:"会员 15 天" 203.0.113.7 10086 月度 foo:bar`)
	want := []struct{ field, kind, value string }{
		{"", SearchNo, "LTD202611020015249524663899"},
		{SearchUser, SearchUser, "张三"},
		{SearchPrize, SearchPrize, "会员 15 天"},
		{"", SearchIP, "203.0.113.7"},
		{"", SearchID, "10086"},
		{"", SearchAny, "月度"},
	}
	if len(terms) != MaxSearchTerms {
		t.Fatalf("最多 %d 段，得到 %d：%+v", MaxSearchTerms, len(terms), terms)
	}
	for index, term := range terms {
		if term.Field != want[index].field || term.Kind() != want[index].kind || term.Value != want[index].value {
			t.Fatalf("第 %d 段 %+v（kind %s），应为 %+v", index, term, term.Kind(), want[index])
		}
	}
	if terms := ParseDrawSearch("foo:bar"); len(terms) != 1 || terms[0].Field != SearchAny || terms[0].Value != "foo:bar" {
		t.Fatalf("未知字段应整体当关键词：%+v", terms)
	}
	if len(terms[0].Value) != DrawNoLength {
		t.Fatal("单号长度常量不对")
	}
}
