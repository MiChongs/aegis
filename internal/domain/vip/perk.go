package vip

import (
	"errors"
	"fmt"
	"strings"
)

// 权益标签：会员页上「这个档位给你什么」的一句话（docs/vip-tiers.md「权益标签」）。
//
// 标签只用于展示，与功能标识（Features）无关：不参与「已拥有」判断、会员折算，也不解锁任何功能。
// 来源两种：按应用当前的计费设置自动生成的（如 AI 调用折扣、会员 AI 额度，改价后自动跟着变），
// 与管理员在档位上手填的补充说明。

// 标签来源。
const (
	PerkSourceAI     = "ai"
	PerkSourceCustom = "custom"
)

// MaxCustomPerks 一个档位最多手填多少个标签。
const MaxCustomPerks = 12

// Perk 一个权益标签。
type Perk struct {
	// Key 稳定键：自动标签为 ai.rate / ai.daily / ai.monthly / ai.pack / ai.limits，手填的为 custom.N
	Key string `json:"key"`
	// Label 标签文字（≤ 16 字），Detail 展开说明（≤ 60 字，可空）
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Source string `json:"source"`
}

// NormalizeCustomPerks 校验并规整手填标签：去空白、去重（按文字）、重编 key。
func NormalizeCustomPerks(in []Perk) ([]Perk, error) {
	out := make([]Perk, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		label := strings.TrimSpace(raw.Label)
		detail := strings.TrimSpace(raw.Detail)
		if label == "" {
			continue
		}
		if n := len([]rune(label)); n > 16 {
			return nil, fmt.Errorf("权益标签「%s」超过 16 个字", label)
		}
		if len([]rune(detail)) > 60 {
			return nil, fmt.Errorf("权益标签「%s」的说明超过 60 个字", label)
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, Perk{Key: fmt.Sprintf("custom.%d", len(out)+1), Label: label, Detail: detail, Source: PerkSourceCustom})
	}
	if len(out) > MaxCustomPerks {
		return nil, errors.New(fmt.Sprintf("每个档位最多 %d 个补充权益标签", MaxCustomPerks))
	}
	return out, nil
}
