package service

import (
	"strings"
	"testing"

	"aegis/internal/domain/aigateway"
	vipdomain "aegis/internal/domain/vip"

	"github.com/shopspring/decimal"
)

func TestAIMemberPerks(t *testing.T) {
	dec := decimal.RequireFromString
	settings := aigateway.DefaultSettings()
	settings.Enabled = true
	settings.VipDaily, settings.VipMonthly = dec("100"), dec("3000")
	settings.TimePricing.Stacking = aigateway.StackBest
	settings.MemberTiers = []aigateway.MemberTier{
		{TierID: 5, Label: "SVIP", Multiplier: dec("0.9"), Daily: dec("200"), Monthly: dec("6000"),
			PackDiscount: dec("0.9"), PackBonusRate: dec("0.05"), RPM: 90, MaxConcurrent: 8},
	}
	labels := func(perks []vipdomain.Perk) string {
		out := make([]string, 0, len(perks))
		for _, perk := range perks {
			out = append(out, perk.Label)
		}
		return strings.Join(out, " | ")
	}
	if got := labels(aiMemberPerks(settings, 5, "SVIP")); got != "AI 调用 9 折 | 每天 200 AI 额度 | 每月 6,000 AI 额度 | 额度包 9 折·多送 5% | AI 并发 8 路" {
		t.Fatalf("SVIP 标签：%s", got)
	}
	// 没配置档位：按通用会员（倍率 1 不出折扣标签，额度包无优惠也不出）
	if got := labels(aiMemberPerks(settings, 99, "其他")); got != "每天 100 AI 额度 | 每月 3,000 AI 额度" {
		t.Fatalf("通用会员标签：%s", got)
	}
	settings.Store.Enabled = false
	if got := labels(aiMemberPerks(settings, 5, "SVIP")); strings.Contains(got, "额度包") {
		t.Fatalf("额度商店关闭时不该出额度包标签：%s", got)
	}
	settings.Enabled = false
	if perks := aiMemberPerks(settings, 5, "SVIP"); len(perks) != 0 {
		t.Fatalf("AI 中转关闭时不该有标签：%v", perks)
	}
	if formatPerkPoints(dec("150000")) != "15 万" || formatPerkPoints(dec("999")) != "999" {
		t.Fatal("额度数格式")
	}
	merged := tierPerks(nil, 5, "SVIP", []vipdomain.Perk{{Key: "custom.1", Label: "专属客服", Source: vipdomain.PerkSourceCustom}})
	if len(merged) != 1 || merged[0].Label != "专属客服" {
		t.Fatalf("读不到计费设置时只剩手填标签：%v", merged)
	}
}

func TestNormalizeCustomPerks(t *testing.T) {
	perks, err := vipdomain.NormalizeCustomPerks([]vipdomain.Perk{{Label: " 专属客服 "}, {Label: "专属客服"}, {Label: ""}, {Label: "生日礼", Detail: "生日当月送 1000 积分"}})
	if err != nil || len(perks) != 2 || perks[1].Key != "custom.2" || perks[0].Source != vipdomain.PerkSourceCustom {
		t.Fatalf("规整结果不对：%+v %v", perks, err)
	}
	if _, err := vipdomain.NormalizeCustomPerks([]vipdomain.Perk{{Label: strings.Repeat("长", 17)}}); err == nil {
		t.Fatal("超过 16 字应报错")
	}
}
