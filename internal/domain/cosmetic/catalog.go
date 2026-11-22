package cosmetic

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// 槽位。
const (
	SlotAvatarFrame = "avatar_frame"
	SlotProfileCard = "profile_card"
	SlotNameStyle   = "name_style"
	SlotBadge       = "badge"
)

// 稀有度，由低到高。
const (
	RarityCommon    = "common"
	RarityRare      = "rare"
	RarityEpic      = "epic"
	RarityLegendary = "legendary"
	RarityMythic    = "mythic"
)

// 状态。
const (
	StatusDraft    = "draft"    // 设计中，用户看不到、不能发放
	StatusActive   = "active"   // 上架
	StatusArchived = "archived" // 下架：用户看不到，已持有的也不能再戴（读取时过滤）
)

// 获取方式。
const (
	UnlockGrant   = "grant"   // 只能经发放获得（积分商城、抽奖、卡密、物品发放、邀请、排行榜、管理员授予）
	UnlockFree    = "free"    // 人人可戴
	UnlockVIP     = "vip"     // 有效会员（可限定档位）
	UnlockLevel   = "level"   // 等级达到
	UnlockFeature = "feature" // 持有某个功能
)

// 持有记录的来源。与功能解锁的来源同名同义，另加管理员授予。
const (
	SourceAdminGrant  = "admin_grant"
	SourcePointsStore = "points_store"
	SourceLottery     = "lottery"
	SourceCardKey     = "card_key"
	SourceItemGrant   = "item_grant"
	SourceInvite      = "invite"
	SourceLeaderboard = "leaderboard"
	SourcePromo       = "promo"
)

// 不能戴的原因。
const (
	ReasonNotOwned     = "not_owned"     // 只能发放获得、还没有
	ReasonExpired      = "expired"       // 有过、已过期或被收回
	ReasonNeedVIP      = "need_vip"      // 需要有效会员
	ReasonNeedTier     = "need_tier"     // 需要指定档位的会员
	ReasonNeedLevel    = "need_level"    // 等级不够
	ReasonNeedFeature  = "need_feature"  // 没有对应功能
	ReasonUnavailable  = "unavailable"   // 装扮已下架或未发布
	ReasonSlotMismatch = "slot_mismatch" // 装扮不属于这个槽位
)

// 获取渠道。
const (
	ChannelPointsStore = "points_store"
	ChannelLottery     = "lottery"
)

// 上界。
const (
	DefaultBadgeSlots = 3
	MaxBadgeSlots     = 5
	MaxGrantDays      = 3650
	MaxNameRunes      = 32
	MaxDescRunes      = 200
	MaxSeriesRunes    = 32
	MaxHintRunes      = 64
	MaxCodeLen        = 48
	MaxItemsPerApp    = 2000
	MaxAssetsPerApp   = 5000
	MaxAssetBytes     = 8 << 20
	MaxPreviewBytes   = 2 << 20
	MaxRevisionsKept  = 50
	MaxLookupUsers    = 100
	MaxNoteRunes      = 128
)

// SlotSpec 一个槽位的自述：服务端校验、控制台设计器的画布预设、客户端的布局都读它。
type SlotSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	// Canvas 新建时的默认画布
	Canvas Size `json:"canvas"`
	// Avatar 新建时头像所在的位置（只有头像框有）
	Avatar *AvatarSlot `json:"avatar,omitempty"`
	// Layered 用图层设计；昵称样式不是图层，是一组文字样式
	Layered bool `json:"layered"`
	// Square 画布必须是正方形
	Square bool `json:"square"`
	// Multiple 可以同时戴几件（勋章），上限由应用设置 badgeSlots 决定
	Multiple bool `json:"multiple"`
	// MinCanvas / MaxCanvas 画布边长范围
	MinCanvas int `json:"minCanvas"`
	MaxCanvas int `json:"maxCanvas"`
}

var slotSpecs = []SlotSpec{
	{Key: SlotAvatarFrame, Label: "头像框", Layered: true, Square: true, MinCanvas: 64, MaxCanvas: 1024,
		Canvas: Size{Width: 300, Height: 300}, Avatar: &AvatarSlot{X: 50, Y: 50, Size: 200, Shape: AvatarCircle},
		Hint: "画在头像四周；头像位于画布中的指定区域，图层可以放在头像之上或之下"},
	{Key: SlotProfileCard, Label: "资料卡背景", Layered: true, MinCanvas: 64, MaxCanvas: 2048,
		Canvas: Size{Width: 1080, Height: 608},
		Hint:   "个人主页与资料卡顶部的背景，客户端按宽度等比缩放、超出部分裁掉"},
	{Key: SlotNameStyle, Label: "昵称样式", Layered: false,
		Hint: "昵称的颜色与动态效果，出现在资料卡、排行榜等显示昵称的地方"},
	{Key: SlotBadge, Label: "勋章", Layered: true, Square: true, Multiple: true, MinCanvas: 32, MaxCanvas: 512,
		Canvas: Size{Width: 96, Height: 96},
		Hint:   "昵称旁的小图标，可以同时佩戴多枚（数量在装扮设置里配置）"},
}

// RaritySpec 稀有度的自述。Color 是控制台与客户端的标识色。
type RaritySpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Color string `json:"color"`
	Rank  int    `json:"rank"`
}

var raritySpecs = []RaritySpec{
	{Key: RarityCommon, Label: "普通", Color: "#8A94A6", Rank: 1},
	{Key: RarityRare, Label: "稀有", Color: "#3B82F6", Rank: 2},
	{Key: RarityEpic, Label: "史诗", Color: "#8B5CF6", Rank: 3},
	{Key: RarityLegendary, Label: "传说", Color: "#F59E0B", Rank: 4},
	{Key: RarityMythic, Label: "神话", Color: "#EF4444", Rank: 5},
}

// UnlockSpec 获取方式的自述。
type UnlockSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

var unlockSpecs = []UnlockSpec{
	{Key: UnlockGrant, Label: "发放获得", Hint: "只能经积分商城、抽奖、卡密、物品发放、邀请、排行榜或管理员授予获得"},
	{Key: UnlockFree, Label: "免费", Hint: "所有用户都可以佩戴"},
	{Key: UnlockVIP, Label: "会员专属", Hint: "有效会员可以佩戴，可以限定会员档位；会员到期后自动失效"},
	{Key: UnlockLevel, Label: "等级解锁", Hint: "等级达到指定值即可佩戴"},
	{Key: UnlockFeature, Label: "功能权益", Hint: "持有指定功能（会员档位、功能解锁）即可佩戴"},
}

// SourceSpec 来源的自述（持有记录列表上显示）。
type SourceSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var sourceSpecs = []SourceSpec{
	{Key: SourceAdminGrant, Label: "管理员授予"},
	{Key: SourcePointsStore, Label: "积分商城"},
	{Key: SourceLottery, Label: "抽奖"},
	{Key: SourceCardKey, Label: "卡密"},
	{Key: SourceItemGrant, Label: "物品发放"},
	{Key: SourceInvite, Label: "邀请"},
	{Key: SourceLeaderboard, Label: "排行榜"},
	{Key: SourcePromo, Label: "推广活动"},
}

// Slots 全部槽位（副本），顺序即展示顺序。
func Slots() []SlotSpec { return slices.Clone(slotSpecs) }

// Rarities 全部稀有度（副本）。
func Rarities() []RaritySpec { return slices.Clone(raritySpecs) }

// UnlockModes 全部获取方式（副本）。
func UnlockModes() []UnlockSpec { return slices.Clone(unlockSpecs) }

// Sources 全部来源（副本）。
func Sources() []SourceSpec { return slices.Clone(sourceSpecs) }

// FindSlot 按键取槽位。
func FindSlot(key string) (SlotSpec, bool) {
	for _, spec := range slotSpecs {
		if spec.Key == key {
			return spec, true
		}
	}
	return SlotSpec{}, false
}

// FindRarity 按键取稀有度。
func FindRarity(key string) (RaritySpec, bool) {
	for _, spec := range raritySpecs {
		if spec.Key == key {
			return spec, true
		}
	}
	return RaritySpec{}, false
}

// FindUnlock 按键取获取方式。
func FindUnlock(key string) (UnlockSpec, bool) {
	for _, spec := range unlockSpecs {
		if spec.Key == key {
			return spec, true
		}
	}
	return UnlockSpec{}, false
}

// SlotLabel 槽位中文名（未知槽位原样返回）。
func SlotLabel(key string) string {
	if spec, ok := FindSlot(key); ok {
		return spec.Label
	}
	return key
}

// SourceLabel 来源中文名。
func SourceLabel(key string) string {
	for _, spec := range sourceSpecs {
		if spec.Key == key {
			return spec.Label
		}
	}
	return key
}

// IsSource 是否已登记的来源。
func IsSource(key string) bool {
	for _, spec := range sourceSpecs {
		if spec.Key == key {
			return true
		}
	}
	return false
}

var codePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_\-.]*$`)

// NormalizeItemInput 去空白、补默认值。
func NormalizeItemInput(in ItemInput) ItemInput {
	in.Slot = strings.TrimSpace(in.Slot)
	in.Code = strings.ToLower(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Rarity = strings.TrimSpace(in.Rarity)
	if in.Rarity == "" {
		in.Rarity = RarityCommon
	}
	in.Series = strings.TrimSpace(in.Series)
	in.Unlock = NormalizeUnlock(in.Unlock)
	return in
}

// NormalizeUnlock 抹掉与模式无关的字段。
func NormalizeUnlock(u Unlock) Unlock {
	clean := Unlock{Mode: strings.TrimSpace(u.Mode), Hint: strings.TrimSpace(u.Hint)}
	if clean.Mode == "" {
		clean.Mode = UnlockGrant
	}
	switch clean.Mode {
	case UnlockVIP:
		seen := make(map[int64]bool, len(u.TierIDs))
		for _, id := range u.TierIDs {
			if id > 0 && !seen[id] {
				seen[id] = true
				clean.TierIDs = append(clean.TierIDs, id)
			}
		}
		slices.Sort(clean.TierIDs)
	case UnlockLevel:
		clean.MinLevel = u.MinLevel
	case UnlockFeature:
		clean.Feature = strings.TrimSpace(u.Feature)
	}
	return clean
}

// ValidateItemInput 校验基本信息（引用的档位与功能是否存在由服务层查）。
func ValidateItemInput(in ItemInput) error {
	if _, ok := FindSlot(in.Slot); !ok {
		return fmt.Errorf("未登记的槽位：%s", in.Slot)
	}
	if in.Code == "" {
		return fmt.Errorf("请填写装扮编码")
	}
	if len(in.Code) > MaxCodeLen || !codePattern.MatchString(in.Code) {
		return fmt.Errorf("装扮编码只能用小写字母、数字、下划线、短横线与点，且以字母或数字开头，最长 %d 位", MaxCodeLen)
	}
	if in.Name == "" {
		return fmt.Errorf("请填写装扮名称")
	}
	if utf8.RuneCountInString(in.Name) > MaxNameRunes {
		return fmt.Errorf("名称不能超过 %d 字", MaxNameRunes)
	}
	if utf8.RuneCountInString(in.Description) > MaxDescRunes {
		return fmt.Errorf("简介不能超过 %d 字", MaxDescRunes)
	}
	if utf8.RuneCountInString(in.Series) > MaxSeriesRunes {
		return fmt.Errorf("系列名不能超过 %d 字", MaxSeriesRunes)
	}
	if _, ok := FindRarity(in.Rarity); !ok {
		return fmt.Errorf("未登记的稀有度：%s", in.Rarity)
	}
	return ValidateUnlock(in.Unlock)
}

// ValidateUnlock 校验获取方式的形状。
func ValidateUnlock(u Unlock) error {
	if _, ok := FindUnlock(u.Mode); !ok {
		return fmt.Errorf("未登记的获取方式：%s", u.Mode)
	}
	if utf8.RuneCountInString(u.Hint) > MaxHintRunes {
		return fmt.Errorf("获取说明不能超过 %d 字", MaxHintRunes)
	}
	switch u.Mode {
	case UnlockLevel:
		if u.MinLevel < 1 || u.MinLevel > 10_000 {
			return fmt.Errorf("等级解锁需要填写 1–10000 之间的等级")
		}
	case UnlockFeature:
		if u.Feature == "" {
			return fmt.Errorf("功能权益需要选择一个功能")
		}
	case UnlockVIP:
		if len(u.TierIDs) > 20 {
			return fmt.Errorf("最多限定 20 个会员档位")
		}
	}
	return nil
}

// ValidateGrantDays 发放天数：0 为永久。
func ValidateGrantDays(days int) error {
	if days < 0 || days > MaxGrantDays {
		return fmt.Errorf("装扮的天数需要在 0–%d 之间（0 为永久）", MaxGrantDays)
	}
	return nil
}

// ValidateSettings 校验应用设置。
func ValidateSettings(s Settings) error {
	if s.BadgeSlots < 1 || s.BadgeSlots > MaxBadgeSlots {
		return fmt.Errorf("勋章佩戴数需要在 1–%d 之间", MaxBadgeSlots)
	}
	return ValidateWorkshopSettings(s.Workshop)
}

// TermLabel 时长的展示口径：永久 / 1 年 / 3 个月 / 7 天。
func TermLabel(days int) string {
	switch {
	case days <= 0:
		return "永久"
	case days%365 == 0:
		return fmt.Sprintf("%d 年", days/365)
	case days%30 == 0:
		return fmt.Sprintf("%d 个月", days/30)
	default:
		return fmt.Sprintf("%d 天", days)
	}
}
