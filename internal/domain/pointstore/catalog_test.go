package pointstore

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestRewardCatalogIsSelfConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range RewardCatalog() {
		if spec.Type == "" || spec.Label == "" {
			t.Errorf("权益档位缺少 type 或 label：%+v", spec)
		}
		if seen[spec.Type] {
			t.Errorf("权益类型重复登记：%s", spec.Type)
		}
		seen[spec.Type] = true
		switch spec.Value {
		case ValueAmount:
			if spec.Min <= 0 || spec.Max <= spec.Min || spec.Unit == "" {
				t.Errorf("%s：整数量档位必须有合理的 Min/Max 与单位", spec.Type)
			}
		case ValueFeature:
			if spec.Max <= 0 || spec.Unit == "" {
				t.Errorf("%s：功能解锁必须有天数上限与单位", spec.Type)
			}
		case ValueMoney, ValueRef, ValueText:
		case ValueCosmetic:
			if spec.Min != 0 || spec.Max <= 0 || spec.Unit == "" {
				t.Errorf("%s：装扮档位需要 0（永久）起的天数范围与单位", spec.Type)
			}
		case ValueNumberTicket:
			if spec.Min <= 0 || spec.Max <= spec.Min || spec.Unit == "" {
				t.Errorf("%s：靓号券档位必须有合理的 Min/Max 与单位", spec.Type)
			}
		case ValueCoupon:
			if spec.Min != 1 || spec.Max <= spec.Min || spec.Unit == "" {
				t.Errorf("%s：优惠券档位需要从 1 张起的张数范围与单位", spec.Type)
			}
		default:
			t.Errorf("%s：未知的取值形态 %q", spec.Type, spec.Value)
		}
	}
	for _, rewardType := range RewardTypes() {
		if _, ok := FindRewardSpec(rewardType); !ok {
			t.Errorf("RewardTypes 里的 %s 在目录里查不到", rewardType)
		}
	}
	// 「功能时长」是激励广告独有的一档，积分商城卖的是「一段时间能用」（功能解锁），不接纳它
	if _, ok := FindRewardSpec("feature_minutes"); ok {
		t.Error("积分商城不应接纳功能时长")
	}
}

func TestValidateRewards(t *testing.T) {
	cases := []struct {
		name    string
		rewards []Reward
		wantErr string
	}{
		{"空清单", nil, "至少要配置一项权益"},
		{"未登记的类型", []Reward{{Type: "integral", Amount: 1}}, "未登记的权益类型"},
		{"会员天数越界", []Reward{{Type: RewardVipDays, Amount: 0}}, "需要在"},
		{"同类重复", []Reward{{Type: RewardVipDays, Amount: 7}, {Type: RewardVipDays, Amount: 7}}, "重复配置"},
		{"功能没选", []Reward{{Type: RewardFeature, Amount: 7}}, "没有选择功能"},
		{"同一功能两次", []Reward{{Type: RewardFeature, Tag: "online_music", Amount: 7}, {Type: RewardFeature, Tag: " ONLINE_MUSIC ", Amount: 0}}, "重复配置"},
		{"功能天数越界", []Reward{{Type: RewardFeature, Tag: "online_music", Amount: -1}}, "0–3650"},
		{"人工发放没写内容", []Reward{{Type: RewardManual, Text: "  "}}, "需要写明"},
		{"人工发放混搭", []Reward{{Type: RewardManual, Text: "周边"}, {Type: RewardVipDays, Amount: 7}}, "只能单独配置"},
		{"余额为零", []Reward{{Type: RewardBalance, Money: decimal.Zero}}, "必须大于 0"},
		{"余额三位小数", []Reward{{Type: RewardBalance, Money: decimal.RequireFromString("1.005")}}, "两位小数"},
		{"套餐没选", []Reward{{Type: RewardVipPlan}}, "没有选择具体套餐"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRewards(tc.rewards)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("期望报「%s」，实际 %v", tc.wantErr, err)
			}
		})
	}

	ok := []Reward{
		{Type: RewardFeature, Tag: "online_music", Amount: 0},
		{Type: RewardFeature, Tag: "doubao_analyze", Amount: 30},
		{Type: RewardVipDays, Amount: 7},
		{Type: RewardBalance, Money: decimal.RequireFromString("1.50")},
	}
	if err := ValidateRewards(ok); err != nil {
		t.Fatalf("合法清单被拒：%v", err)
	}
}

// 重排按目录顺序，多个功能解锁保持相对顺序；与取值形态无关的字段被抹掉。
func TestNormalizeRewards(t *testing.T) {
	got := NormalizeRewards([]Reward{
		{Type: RewardFeature, Tag: "B_TAG", Amount: 3, RefID: 9},
		{Type: RewardVipDays, Amount: 7, Tag: "x"},
		{Type: RewardFeature, Tag: "a_tag", Amount: 0},
	})
	if len(got) != 3 || got[0].Type != RewardVipDays || got[1].Tag != "b_tag" || got[2].Tag != "a_tag" {
		t.Fatalf("重排结果不对：%+v", got)
	}
	if got[0].Tag != "" || got[1].RefID != 0 {
		t.Errorf("无关字段应被抹掉：%+v", got)
	}
}

func baseItem() Item {
	return Item{ID: 1, Title: "在线音乐 7 天", Cost: 100, Status: StatusActive, LimitPeriod: PeriodTotal,
		Audience: AudienceAll, Rewards: []Reward{{Type: RewardFeature, Tag: "online_music", Amount: 7}}}
}

func baseFacts() UserFacts {
	return UserFacts{Integral: 500, Level: 3,
		Features: map[string]FeatureRef{"online_music": {Tag: "online_music", Name: "在线音乐", IsActive: true}},
		Plans:    map[int64]PlanRef{}}
}

func TestEvaluateReasons(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	stock := func(v int64) *int64 { return &v }
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }

	cases := []struct {
		name   string
		item   func(*Item)
		facts  func(*UserFacts)
		usage  Usage
		reason string
	}{
		{"可以兑换", nil, nil, Usage{}, ReasonOK},
		{"下架", func(i *Item) { i.Status = StatusInactive }, nil, Usage{}, ReasonInactive},
		{"未开始", func(i *Item) { i.StartsAt = at(time.Hour) }, nil, Usage{}, ReasonNotStarted},
		{"已结束", func(i *Item) { i.EndsAt = at(0) }, nil, Usage{}, ReasonEnded},
		{"功能被停用", nil, func(f *UserFacts) { f.Features["online_music"] = FeatureRef{Tag: "online_music"} }, Usage{}, ReasonRewardUnavailable},
		{"已兑完", func(i *Item) { i.Stock = stock(0) }, nil, Usage{}, ReasonSoldOut},
		{"本周已满", func(i *Item) { i.LimitPeriod, i.LimitCount = PeriodWeek, 2 }, nil, Usage{Total: 5, Week: 2}, ReasonLimitReached},
		{"本周未满", func(i *Item) { i.LimitPeriod, i.LimitCount = PeriodWeek, 2 }, nil, Usage{Total: 5, Week: 1}, ReasonOK},
		{"仅会员", func(i *Item) { i.Audience = AudienceVip }, nil, Usage{}, ReasonVipOnly},
		{"仅非会员", func(i *Item) { i.Audience = AudienceNonVip }, func(f *UserFacts) { f.IsVip = true }, Usage{}, ReasonNonVipOnly},
		{"仅永久会员（限时会员）", func(i *Item) { i.Audience = AudienceLifetime }, func(f *UserFacts) { f.IsVip = true }, Usage{}, ReasonLifetimeOnly},
		{"仅永久会员", func(i *Item) {
			i.Audience, i.Rewards = AudienceLifetime, []Reward{{Type: RewardLotteryDraws, Amount: 30}}
		},
			func(f *UserFacts) { f.IsVip, f.IsLifetime, f.Holdings.Lifetime = true, true, true }, Usage{}, ReasonOK},
		{"等级不足", func(i *Item) { i.MinLevel = 5 }, nil, Usage{}, ReasonLevelRequired},
		{"已永久解锁", nil, func(f *UserFacts) { f.Holdings.PermanentUnlocks = []string{"online_music"} }, Usage{}, ReasonOwned},
		{"永久会员已含该功能", nil, func(f *UserFacts) {
			f.IsLifetime, f.Holdings.Lifetime, f.Holdings.LifetimeFeatures = true, true, []string{"online_music"}
		}, Usage{}, ReasonOwned},
		{"积分不足", func(i *Item) { i.Cost = 501 }, nil, Usage{}, ReasonInsufficientPoints},
		// 优先级：下架的商品先说下架，不说积分不够
		{"下架且积分不足", func(i *Item) { i.Status, i.Cost = StatusInactive, 9999 }, nil, Usage{}, ReasonInactive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item, facts := baseItem(), baseFacts()
			if tc.item != nil {
				tc.item(&item)
			}
			if tc.facts != nil {
				tc.facts(&facts)
			}
			got := Evaluate(item, facts, tc.usage, ClockAt(now, time.UTC))
			if got.Reason != tc.reason || got.Redeemable != (tc.reason == ReasonOK) || got.Message == "" {
				t.Fatalf("期望 %s，实际 %+v", tc.reason, got)
			}
		})
	}
}

// 永久会员兑换会员天数、或已包含全部功能的套餐，都是纯花积分。
func TestOwnedMembership(t *testing.T) {
	facts := baseFacts()
	facts.IsLifetime, facts.Holdings.Lifetime, facts.Holdings.LifetimeFeatures = true, true, []string{"export"}
	facts.Holdings.LifetimeAdFree = true
	facts.Plans = map[int64]PlanRef{
		1: {ID: 1, Name: "基础版", DurationDays: 30, Features: []string{"export"}},
		2: {ID: 2, Name: "高级版", DurationDays: 30, Features: []string{"export", "ai.chat"}},
	}
	if !Owned([]Reward{{Type: RewardVipDays, Amount: 7}}, facts) {
		t.Error("永久会员兑换会员天数应判为已拥有")
	}
	if !Owned([]Reward{{Type: RewardVipPlan, RefID: 1}}, facts) {
		t.Error("功能都已包含的套餐应判为已拥有")
	}
	if Owned([]Reward{{Type: RewardVipPlan, RefID: 2}}, facts) {
		t.Error("多出功能的套餐是正常的升级路径，不该拦")
	}
	facts.IsLifetime, facts.Holdings.Lifetime = false, false
	if Owned([]Reward{{Type: RewardVipDays, Amount: 7}}, facts) {
		t.Error("不是永久会员时会员天数不算已拥有")
	}
}

func TestWindowsAt(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	// 2026-10-12 是周一；UTC 周日 18:00 已经是本地周一凌晨两点
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	w := WindowsAt(now, loc)
	if !w.Day.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, loc)) {
		t.Errorf("自然日起点不对：%v", w.Day)
	}
	if !w.Week.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, loc)) {
		t.Errorf("ISO 周从周一开始：%v", w.Week)
	}
	if !w.Month.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, loc)) {
		t.Errorf("自然月起点不对：%v", w.Month)
	}
	sunday := WindowsAt(time.Date(2026, 10, 18, 10, 0, 0, 0, loc), loc)
	if !sunday.Week.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, loc)) {
		t.Errorf("周日属于本周（周一起），实际 %v", sunday.Week)
	}
}

func TestValidateItemInput(t *testing.T) {
	cost := int64(80)
	input := NormalizeItemInput(ItemInput{Title: " 会员 7 天 ", Cost: 100, OriginalCost: &cost,
		Rewards: []Reward{{Type: RewardVipDays, Amount: 7}}})
	if err := ValidateItemInput(input); err == nil || !strings.Contains(err.Error(), "划线价") {
		t.Fatalf("划线价低于兑换积分应被拒：%v", err)
	}
	input.OriginalCost = nil
	if err := ValidateItemInput(input); err != nil {
		t.Fatalf("合法商品被拒：%v", err)
	}
	if input.Title != "会员 7 天" || input.LimitPeriod != PeriodTotal || input.Audience != AudienceAll || input.Status != StatusActive {
		t.Errorf("清洗与默认值不对：%+v", input)
	}
}

// 摘要要能脱离标签单独成立：客户端把几项摘要连成一行时，「3 天」「500 点」读不出是什么。
func TestDescribeRewardIsSelfContained(t *testing.T) {
	plans := map[int64]PlanRef{1: {ID: 1, Name: "月度会员", DurationDays: 30}}
	features := map[string]FeatureRef{"online_music": {Tag: "online_music", Name: "在线音乐", IsActive: true}}
	cases := map[string]Reward{
		"月度会员 1 个月": {Type: RewardVipPlan, RefID: 1},
		"会员 3 天":    {Type: RewardVipDays, Amount: 3},
		"在线音乐 永久":   {Type: RewardFeature, Tag: "online_music"},
		"经验 500 点":  {Type: RewardExperience, Amount: 500},
		"余额 1.50":   {Type: RewardBalance, Money: decimal.RequireFromString("1.5")},
		"抽奖 3 次":    {Type: RewardLotteryDraws, Amount: 3},
		"定制贴纸":      {Type: RewardManual, Text: "定制贴纸"},
	}
	for want, reward := range cases {
		if got := DescribeReward(reward, plans, features).Detail; got != want {
			t.Errorf("%s：期望「%s」，实际「%s」", reward.Type, want, got)
		}
	}
}
