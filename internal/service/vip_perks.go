package service

import (
	"context"
	"fmt"
	"strings"

	"aegis/internal/domain/aigateway"
	vipdomain "aegis/internal/domain/vip"

	"github.com/shopspring/decimal"
)

// 会员档位的权益标签（docs/vip-tiers.md「权益标签」）：按 AI 计费设置自动生成的在前，档位上手填的在后。
// 自动标签读的就是计费时用的同一份 aigateway.Settings.Member 口径，改价后标签自动跟着变。

// aiMemberPerks 某个档位在 AI 中转上的会员优惠，写成标签。AI 中转没开时没有。
// tierID 没在 memberTiers 里配置时按通用会员优惠（与计费一致）。
func aiMemberPerks(settings aigateway.Settings, tierID int64, tierName string) []vipdomain.Perk {
	if !settings.Enabled {
		return nil
	}
	member := settings.VipMember()
	for _, tier := range settings.MemberTiers {
		if tier.TierID == tierID {
			member = settings.TierMember(tier, tierName)
			break
		}
	}
	one := decimal.NewFromInt(1)
	out := make([]vipdomain.Perk, 0, 5)
	add := func(key, label, detail string) {
		out = append(out, vipdomain.Perk{Key: key, Label: label, Detail: detail, Source: vipdomain.PerkSourceAI})
	}
	if zhe := aigateway.ZheLabel(member.Multiplier); zhe != "" {
		add("ai.rate", "AI 调用 "+zhe, "Voyage AI 与 API 调用按 "+zhe+"计费；与夜间等时段优惠"+stackingNote(settings))
	}
	if member.Daily.Sign() > 0 {
		add("ai.daily", "每天 "+formatPerkPoints(member.Daily)+" AI 额度", "每天零点重置，当天用不完不累积")
	}
	if member.Monthly.Sign() > 0 {
		add("ai.monthly", "每月 "+formatPerkPoints(member.Monthly)+" AI 额度", "每个自然月发放，当月有效")
	}
	pack := []string{}
	if zhe := aigateway.ZheLabel(member.PackDiscount); zhe != "" && member.PackDiscount.LessThan(one) {
		pack = append(pack, zhe)
	}
	if member.PackBonusRate.Sign() > 0 {
		pack = append(pack, "多送 "+member.PackBonusRate.Mul(decimal.NewFromInt(100)).Round(1).String()+"%")
	}
	if len(pack) > 0 && settings.Store.Enabled {
		add("ai.pack", "额度包 "+strings.Join(pack, "·"), "用余额购买 AI 额度包时享受（标注不参与会员折扣的额度包除外）")
	}
	rpm, concurrency := settings.EffectiveRPM(member), settings.EffectiveConcurrency(member)
	if rpm > settings.RPM || concurrency > settings.MaxConcurrent {
		detail := fmt.Sprintf("同时 %d 个请求", concurrency)
		if rpm > 0 {
			detail = fmt.Sprintf("每分钟最多 %d 次调用、%s", rpm, detail)
		}
		add("ai.limits", fmt.Sprintf("AI 并发 %d 路", concurrency), detail)
	}
	return out
}

func stackingNote(settings aigateway.Settings) string {
	if settings.TimePricing.Stacking == aigateway.StackBest {
		return "不叠加，取更优惠的一项"
	}
	return "叠加"
}

// formatPerkPoints 额度数写成「6,000」「1.5 万」。
func formatPerkPoints(points decimal.Decimal) string {
	if points.GreaterThanOrEqual(decimal.NewFromInt(100_000)) {
		return points.Div(decimal.NewFromInt(10_000)).Round(1).String() + " 万"
	}
	text := fmt.Sprint(points.Round(0).IntPart())
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

// tierPerks 一个档位的全部标签：自动的在前、手填的在后（手填与自动同字的去掉）。
func tierPerks(settings *aigateway.Settings, tierID int64, tierName string, custom []vipdomain.Perk) []vipdomain.Perk {
	out := []vipdomain.Perk{}
	if settings != nil && tierID > 0 {
		out = append(out, aiMemberPerks(*settings, tierID, tierName)...)
	}
	for _, perk := range custom {
		duplicate := false
		for _, existing := range out {
			if existing.Label == perk.Label {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, perk)
		}
	}
	return out
}

// attachPlanPerks 给在售套餐填上所属档位的权益标签。读不到计费设置或档位时只是少几个标签，不让列表失败。
func (s *VipService) attachPlanPerks(ctx context.Context, appID int64, plans []vipdomain.Plan) {
	briefs, err := s.pg.ListVipTierBriefs(ctx, appID)
	if err != nil {
		s.log.Warn("vip tier perks unavailable")
		return
	}
	var settings *aigateway.Settings
	if loaded, err := s.pg.GetAIGatewaySettings(ctx, appID); err == nil {
		settings = loaded
	}
	cache := map[int64][]vipdomain.Perk{}
	for i := range plans {
		tierID := plans[i].TierID
		if tierID <= 0 {
			continue
		}
		perks, ok := cache[tierID]
		if !ok {
			brief := briefs[tierID]
			perks = tierPerks(settings, tierID, brief.Name, brief.Perks)
			cache[tierID] = perks
		}
		plans[i].Perks = perks
	}
}

// fillAutoPerks 管理端档位列表：填上自动生成的标签，供控制台预览。
func (s *VipService) fillAutoPerks(ctx context.Context, appID int64, tiers []vipdomain.Tier) {
	settings, err := s.pg.GetAIGatewaySettings(ctx, appID)
	if err != nil {
		return
	}
	for i := range tiers {
		tiers[i].AutoPerks = aiMemberPerks(*settings, tiers[i].ID, tiers[i].Name)
		if tiers[i].AutoPerks == nil {
			tiers[i].AutoPerks = []vipdomain.Perk{}
		}
	}
}
