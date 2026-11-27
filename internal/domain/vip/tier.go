package vip

import (
	"slices"
	"time"
)

// 会员档位：权益的唯一来源。
//
// ── 为什么要把权益从套餐里拿出来 ──
//
// 权益原先跟着套餐走（Segment.Plan），套餐一删就定格成开通时的快照。运营整理商品目录时
// 最自然的一步 —— 删掉旧套餐、重新上架一个 —— 会让新套餐成为另一行，老会员与它毫无关系；
// 不挂套餐的自定义发放（管理员赠送永久会员）更是永远停在发放那一刻的功能。
// 「删掉套餐」这个整理目录的动作，实际改的是所有存量会员的权益。
//
// 现在套餐只是档位的**售卖规格**（价格 × 时长 / 永久），功能配在档位上；
// 每段会员期记的是档位，权益按档位**当前**的功能算。删套餐、重新上架、调价、换名都不碰权益；
// 档位不能被直接删掉 —— 还有套餐或会员时必须指定迁往哪个档位，权益不会悬空。
//
// 自定义发放可以在档位之外另给功能（Segment.Extras），不挂档位的发放（会员天数、
// 卡密赠送天数）仍按开通时的功能快照，与以前一样。

// Tier 会员档位。
type Tier struct {
	ID          int64    `json:"id"`
	AppID       int64    `json:"appid"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Features    []string `json:"features"`
	// Perks 管理员手填的补充权益标签（只用于展示，不参与「已拥有」与折算）；
	// AutoPerks 按应用当前的 AI 计费设置自动生成的标签，只在管理端列表里填，用于预览。
	Perks     []Perk    `json:"perks"`
	AutoPerks []Perk    `json:"autoPerks,omitempty"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// 以下仅管理端列表填充
	Plans []TierPlanRef `json:"plans"`
	// Members 此刻有仍生效会员期落在这个档位的人数；LifetimeMembers 其中永久的
	Members         int64 `json:"members"`
	LifetimeMembers int64 `json:"lifetimeMembers"`
	// DefaultLifetime 不挂套餐的永久会员（自定义发放、老系统迁入）默认归入的档位
	DefaultLifetime bool `json:"defaultLifetime"`
}

// TierPlanRef 档位下的一个套餐（列表展示用）。
type TierPlanRef struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Lifetime bool   `json:"lifetime"`
	IsActive bool   `json:"isActive"`
}

// TierMutation 档位创建 / 更新（指针为空表示不变更）。
type TierMutation struct {
	ID          int64
	AppID       int64
	Name        *string
	Description *string
	// Features nil 表示不变更；空切片表示清空
	Features *[]string
	// Perks 手填的补充权益标签；nil 表示不变更
	Perks     *[]Perk
	SortOrder *int
}

// SegmentTier 一段会员期所属档位的当前配置（判定只需要这两项）。
type SegmentTier struct {
	Name     string   `json:"name"`
	Features []string `json:"features"`
}

// TierFeatures 档位功能并上档位之外另给的功能。
func TierFeatures(tier []string, extras []string) []string {
	return NormalizeFeatureTags(append(slices.Clone(tier), extras...))
}
