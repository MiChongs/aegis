package cardkey

import (
	"strings"
	"testing"

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
			// 缺了范围的整数量档位等于没有上下界，控制台的数字输入框也就没有 min/max，
			// 于是「会员 999999 天」这种手滑要到发卡之后才被发现。
			if spec.Min <= 0 || spec.Max <= spec.Min {
				t.Errorf("%s：整数量档位必须有合理的 Min/Max，当前 %d–%d", spec.Type, spec.Min, spec.Max)
			}
			if spec.Unit == "" {
				t.Errorf("%s：整数量档位必须有单位，否则控制台上是一个没有量纲的数字", spec.Type)
			}
		case ValueMoney, ValueRef:
		default:
			t.Errorf("%s：未知的取值形态 %q", spec.Type, spec.Value)
		}
	}

	for _, rewardType := range RewardTypes() {
		if _, ok := FindRewardSpec(rewardType); !ok {
			t.Errorf("RewardTypes 里的 %s 在目录里查不到", rewardType)
		}
	}
}

func TestValidateRewards(t *testing.T) {
	cases := []struct {
		name    string
		rewards []Reward
		wantErr string
	}{
		{
			name:    "空清单",
			rewards: nil,
			wantErr: "至少要配置一项权益",
		},
		{
			name:    "未登记的类型",
			rewards: []Reward{{Type: "vip_dayss", Amount: 30}},
			wantErr: "未登记的权益类型",
		},
		{
			name:    "同一档重复配置",
			rewards: []Reward{{Type: RewardIntegral, Amount: 100}, {Type: RewardIntegral, Amount: 200}},
			wantErr: "重复配置",
		},
		{
			name:    "整数量超出上界",
			rewards: []Reward{{Type: RewardVipDays, Amount: 99999}},
			wantErr: "需要在",
		},
		{
			name:    "整数量为零",
			rewards: []Reward{{Type: RewardVipDays, Amount: 0}},
			wantErr: "需要在",
		},
		{
			name:    "金额为零",
			rewards: []Reward{{Type: RewardBalance, Money: decimal.Zero}},
			wantErr: "必须大于 0",
		},
		{
			name:    "金额超出上界",
			rewards: []Reward{{Type: RewardBalance, Money: decimal.NewFromInt(2_000_000)}},
			wantErr: "不能超过",
		},
		{
			name:    "引用未选择",
			rewards: []Reward{{Type: RewardVipPlan}},
			wantErr: "没有选择具体对象",
		},
		{
			name: "组合权益",
			rewards: []Reward{
				{Type: RewardVipPlan, RefID: 3},
				{Type: RewardIntegral, Amount: 500},
				{Type: RewardBalance, Money: decimal.RequireFromString("9.90")},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRewards(tc.rewards)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("期望通过，实际报错：%v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("期望报错含 %q，实际通过", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("错误文案不含 %q：%v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateRewardsRejectsTooMany(t *testing.T) {
	rewards := []Reward{
		{Type: RewardVipPlan, RefID: 1},
		{Type: RewardVipDays, Amount: 1},
		{Type: RewardIntegral, Amount: 1},
		{Type: RewardExperience, Amount: 1},
		{Type: RewardBalance, Money: decimal.NewFromInt(1)},
		{Type: RewardLotteryDraws, Amount: 1},
		{Type: RewardDeviceSlots, Amount: 1},
	}
	if err := ValidateRewards(rewards); err == nil {
		t.Fatalf("七档权益应当超出 MaxRewardsPerCard=%d", MaxRewardsPerCard)
	}
}

// NormalizeRewards 有两个职责，各自都会在别处引发难查的问题，所以分别断言。
func TestNormalizeRewardsSortsAndCleans(t *testing.T) {
	input := []Reward{
		{Type: RewardIntegral, Amount: 500, RefID: 7, Money: decimal.NewFromInt(3)},
		{Type: RewardVipPlan, RefID: 3, Amount: 99},
	}
	got := NormalizeRewards(input)

	if len(got) != 2 {
		t.Fatalf("期望 2 项，实际 %d", len(got))
	}
	// 排序按目录：会员在积分之前，任何地方的展示顺序才是一致的。
	if got[0].Type != RewardVipPlan || got[1].Type != RewardIntegral {
		t.Fatalf("未按目录顺序重排：%+v", got)
	}
	// 与取值形态无关的字段要被抹掉，否则读日志的人会以为 refId 起了作用。
	if got[1].RefID != 0 || !got[1].Money.IsZero() {
		t.Fatalf("整数量档位残留了无关字段：%+v", got[1])
	}
	if got[0].Amount != 0 {
		t.Fatalf("引用档位残留了 amount：%+v", got[0])
	}
}

func TestDescribeRewardCoversEveryValueKind(t *testing.T) {
	cases := map[string]Reward{
		"会员天数 30 天": {Type: RewardVipDays, Amount: 30},
		"钱包余额 9.90": {Type: RewardBalance, Money: decimal.RequireFromString("9.9")},
		"会员套餐":      {Type: RewardVipPlan, RefID: 3},
	}
	for want, reward := range cases {
		if got := DescribeReward(reward); got != want {
			t.Errorf("DescribeReward(%s) = %q，期望 %q", reward.Type, got, want)
		}
	}
}
