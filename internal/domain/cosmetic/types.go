// Package cosmetic 个性装扮（docs/cosmetics.md）：头像框、资料卡背景、昵称样式、勋章。
//
// ── 三张账 ──
//
//	装扮（cosmetic_items）    运营在控制台设计器里做的东西：槽位、稀有度、获取方式、设计稿（图层 + 动画 + 素材）
//	持有（user_cosmetics）    谁在什么时候从哪里得到了哪一件、到什么时候：与功能解锁同构的账本行，限时顺延、永久无终点
//	佩戴（user_cosmetic_equips）每个槽位当前戴着哪一件（勋章可以戴几枚）
//
// 「能不能戴」只有一个判据（Eligibility）：持有一条仍生效的记录，或者满足装扮自己的获取条件
// （免费 / 会员 / 指定会员档位 / 等级 / 持有功能）。佩戴时判一次、**读取时再判一次**：
// 会员到期、限时装扮过期、装扮下架、被收回之后，库里那一行佩戴记录不必有人去清，
// 读出来就是空的——不需要任何后台扫描，也不存在「过期了但还挂着」的窗口。
//
// ── 资源全部服务端下发 ──
//
// 装扮的外观是一份设计稿（Design，JSON）：画布、头像所在的位置、图层（图片 / 序列帧 / 文字 / 形状 / 粒子）、
// 每个图层的关键帧轨道。素材文件存在应用的对象存储里（cosmetic_assets），设计稿只引用素材编号，
// 下发时换成可直接访问的地址。客户端按设计稿实时绘制，不内置任何一件装扮；
// 改了设计稿重新发布，版本号 +1，客户端据此丢弃缓存。
package cosmetic

import (
	"time"
)

// Item 一件装扮。
type Item struct {
	ID          int64  `json:"id"`
	AppID       int64  `json:"appid,omitempty"`
	Slot        string `json:"slot"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Rarity      string `json:"rarity"`
	Series      string `json:"series,omitempty"`
	Status      string `json:"status"`
	// Unlock 获取方式；持有记录（各系统发放）任何时候都算数，Unlock 只是在持有之外再放宽
	Unlock Unlock `json:"unlock"`
	// Design 已发布的设计稿（下发给客户端的那一份）；从未发布过时为 nil
	Design *Design `json:"design,omitempty"`
	// Draft 设计器里的草稿；只在管理端返回
	Draft *Design `json:"draft,omitempty"`
	// Revision 已发布的版本号，从 1 开始；0 表示还没发布过
	Revision int `json:"revision"`
	// DraftDirty 草稿与已发布的版本不同（控制台据此提示「有未发布的修改」）
	DraftDirty bool `json:"draftDirty,omitempty"`
	// Preview 静态预览图（设计器发布时渲染上传的 PNG），列表与不支持动态绘制的客户端用
	Preview    string `json:"preview,omitempty"`
	PreviewRef string `json:"-"`
	SortOrder  int    `json:"sortOrder"`
	// Hidden 不在装扮中心列出（仍可发放、持有者仍可佩戴）：活动限定、补偿专用
	Hidden      bool       `json:"hidden"`
	CreatedBy   string     `json:"createdBy,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`

	// Origin official 官方 / workshop 工坊作品；CreatorUserID 工坊作品的作者
	Origin        string `json:"origin,omitempty"`
	CreatorUserID int64  `json:"creatorUserId,omitempty"`
	// TakedownReason 工坊作品被运营下架的原因
	TakedownReason string `json:"takedownReason,omitempty"`

	// 以下仅管理端列表填充
	Owners   int64 `json:"owners,omitempty"`
	Equipped int64 `json:"equipped,omitempty"`
}

// Live 装扮此刻对用户生效：已上架且发布过。
func (i Item) Live() bool { return i.Status == StatusActive && i.Revision > 0 && i.Design != nil }

// Workshop 是用户在工坊里做的作品。
func (i Item) Workshop() bool { return i.Origin == OriginWorkshop }

// Unlock 获取方式。
type Unlock struct {
	Mode string `json:"mode"`
	// TierIDs vip 模式下限定的会员档位，为空即任意有效会员
	TierIDs []int64 `json:"tierIds,omitempty"`
	// MinLevel level 模式下的最低等级
	MinLevel int `json:"minLevel,omitempty"`
	// Feature feature 模式下需要持有的功能标识
	Feature string `json:"feature,omitempty"`
	// Hint 给用户看的获取说明（「积分商城兑换」「年度活动前十名」）；为空时客户端按模式生成
	Hint string `json:"hint,omitempty"`
}

// Ownership 一条持有记录（账本行）。
type Ownership struct {
	ID         int64  `json:"id"`
	AppID      int64  `json:"appid,omitempty"`
	UserID     int64  `json:"userId,omitempty"`
	CosmeticID int64  `json:"cosmeticId"`
	Source     string `json:"source"`
	SourceRef  string `json:"sourceRef,omitempty"`
	// DurationDays 0 表示永久
	DurationDays int        `json:"durationDays"`
	StartsAt     time.Time  `json:"startsAt"`
	EndsAt       *time.Time `json:"endsAt,omitempty"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
	RevokedBy    string     `json:"revokedBy,omitempty"`
	RevokeReason string     `json:"revokeReason,omitempty"`
	Operator     string     `json:"operator,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`

	// 以下为列表展示附带
	CosmeticName string `json:"cosmeticName,omitempty"`
	Slot         string `json:"slot,omitempty"`
	Rarity       string `json:"rarity,omitempty"`
	UserAccount  string `json:"userAccount,omitempty"`
	UserNickname string `json:"userNickname,omitempty"`
}

// Permanent 永久持有。
func (o Ownership) Permanent() bool { return o.EndsAt == nil }

// LiveAt 这条记录在 now 时刻是否仍算持有。与功能解锁同理不要求 StartsAt <= now：
// 第二段七天排在第一段之后，但这件装扮此刻本来就在手里。
func (o Ownership) LiveAt(now time.Time) bool {
	if o.RevokedAt != nil {
		return false
	}
	return o.EndsAt == nil || o.EndsAt.After(now)
}

// Equip 一个槽位上的佩戴。
type Equip struct {
	Slot       string    `json:"slot"`
	Position   int       `json:"position"`
	CosmeticID int64     `json:"cosmeticId"`
	EquippedAt time.Time `json:"equippedAt"`
}

// Settings 应用级装扮设置。
type Settings struct {
	AppID   int64 `json:"appid,omitempty"`
	Enabled bool  `json:"enabled"`
	// BadgeSlots 最多同时佩戴几枚勋章（1–MaxBadgeSlots）
	BadgeSlots int `json:"badgeSlots"`
	// Workshop 自定义工坊
	Workshop  WorkshopSettings `json:"workshop"`
	UpdatedBy string           `json:"updatedBy,omitempty"`
	UpdatedAt *time.Time       `json:"updatedAt,omitempty"`
}

// DefaultSettings 没有配置过的应用：关闭，勋章三枚。
func DefaultSettings(appID int64) Settings {
	return Settings{AppID: appID, Enabled: false, BadgeSlots: DefaultBadgeSlots}
}

// Asset 素材库里的一个文件（同一应用的装扮共用）。
type Asset struct {
	ID          int64     `json:"id"`
	AppID       int64     `json:"appid,omitempty"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	ContentType string    `json:"contentType"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	Reference   string    `json:"-"`
	URL         string    `json:"url"`
	UploadedBy  string    `json:"uploadedBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	// UsedBy 引用它的装扮数（管理端列表填充；被引用的素材不能删）
	UsedBy int64 `json:"usedBy"`
}

// Revision 一次发布留下的版本。
type Revision struct {
	ID          int64     `json:"id"`
	CosmeticID  int64     `json:"cosmeticId"`
	Revision    int       `json:"revision"`
	Design      *Design   `json:"design,omitempty"`
	Note        string    `json:"note,omitempty"`
	PublishedBy string    `json:"publishedBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ItemInput 新建 / 更新装扮的基本信息（设计稿走单独的草稿接口）。
type ItemInput struct {
	AppID       int64
	Slot        string
	Code        string
	Name        string
	Description string
	Rarity      string
	Series      string
	Unlock      Unlock
	SortOrder   int
	Hidden      bool
	Operator    string
}

// GrantInput 发一件装扮（管理员授予或其它系统的发放分支）。
type GrantInput struct {
	AppID      int64
	UserID     int64
	CosmeticID int64
	// Days 0 为永久；同一件多次限时发放时顺延
	Days      int
	Source    string
	SourceRef string
	Operator  string
}

// GrantResult 发放结果。
type GrantResult struct {
	Ownership *Ownership `json:"ownership,omitempty"`
	// AlreadyPermanent 已永久持有，这次没有记新的一段（发放方按「已拥有」处理，不算失败）
	AlreadyPermanent bool   `json:"alreadyPermanent,omitempty"`
	CosmeticName     string `json:"cosmeticName"`
	Slot             string `json:"slot"`
}

// Facts 判定「能不能戴」所需的用户事实，由服务层一次取齐。
type Facts struct {
	// UserID 判工坊作品「是不是作者本人」
	UserID int64
	// WorkshopQuota 此刻的工坊额度（服务层按应用设置算好填入；0 即作品戴不上）
	WorkshopQuota int
	IsVIP         bool
	// TierIDs 此刻仍生效的会员期所属档位
	TierIDs  []int64
	Level    int
	Features []string
	// Owned 此刻仍算持有的装扮：编号 → 最晚到期（nil 永久）
	Owned map[int64]*time.Time
}

// View 客户端看到的一件装扮（装扮中心、佩戴结果、别人身上的装扮都是它）。
type View struct {
	ID          int64   `json:"id"`
	Slot        string  `json:"slot"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Rarity      string  `json:"rarity"`
	Series      string  `json:"series,omitempty"`
	Revision    int     `json:"revision"`
	Preview     string  `json:"preview,omitempty"`
	Design      *Design `json:"design,omitempty"`
	// Workshop 用户在工坊里做的作品（客户端可以标一个「工坊」角标）
	Workshop bool `json:"workshop,omitempty"`
}

// CenterItem 装扮中心的一件：外观 + 我能不能戴、为什么。
type CenterItem struct {
	View
	Unlock UnlockView `json:"unlock"`
	State  ItemState  `json:"state"`
	// Channels 去哪儿能得到它（上架中的积分商城商品、进行中的抽奖活动）
	Channels []Channel `json:"channels,omitempty"`
}

// UnlockView 获取方式的客户端视图。
type UnlockView struct {
	Mode     string `json:"mode"`
	Label    string `json:"label"`
	Hint     string `json:"hint,omitempty"`
	MinLevel int    `json:"minLevel,omitempty"`
	Feature  string `json:"feature,omitempty"`
	// Tiers 限定的会员档位名称
	Tiers []string `json:"tiers,omitempty"`
}

// ItemState 我与这件装扮的关系。
type ItemState struct {
	// Usable 此刻能戴
	Usable bool `json:"usable"`
	// Owned 有仍生效的持有记录；Permanent 其中有永久的
	Owned     bool       `json:"owned"`
	Permanent bool       `json:"permanent"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// Reason 不能戴的原因（Reason* 常量）；能戴时为空
	Reason string `json:"reason,omitempty"`
	// Equipped 正戴在身上；Position 勋章戴在第几位
	Equipped bool `json:"equipped"`
	Position int  `json:"position,omitempty"`
}

// Channel 获取渠道。
type Channel struct {
	Kind  string `json:"kind"`
	RefID int64  `json:"refId"`
	Title string `json:"title"`
	// Cost 积分商城的积分价
	Cost int64 `json:"cost,omitempty"`
	// Days 这个渠道给的天数，0 为永久
	Days int `json:"days"`
}

// Loadout 一个人此刻身上的全部装扮（已按 Eligibility 过滤，失效的不出现）。
type Loadout struct {
	AvatarFrame *View  `json:"avatarFrame,omitempty"`
	ProfileCard *View  `json:"profileCard,omitempty"`
	NameStyle   *View  `json:"nameStyle,omitempty"`
	Badges      []View `json:"badges"`
}

// Empty 身上什么都没戴。
func (l Loadout) Empty() bool {
	return l.AvatarFrame == nil && l.ProfileCard == nil && l.NameStyle == nil && len(l.Badges) == 0
}

// Stats 管理端统计。
type Stats struct {
	Items         int64           `json:"items"`
	LiveItems     int64           `json:"liveItems"`
	Owners        int64           `json:"owners"`
	Ownerships    int64           `json:"ownerships"`
	Wearers       int64           `json:"wearers"`
	BySlot        []SlotStat      `json:"bySlot"`
	BySource      []SourceStat    `json:"bySource"`
	TopItems      []ItemStat      `json:"topItems"`
	RecentGrants  []Ownership     `json:"recentGrants"`
	DailyGrants   []DailyGrantRow `json:"dailyGrants"`
	ExpiringSoon  int64           `json:"expiringSoon"`
	AssetCount    int64           `json:"assetCount"`
	AssetBytes    int64           `json:"assetBytes"`
	DraftsPending int64           `json:"draftsPending"`
}

type SlotStat struct {
	Slot     string `json:"slot"`
	Items    int64  `json:"items"`
	Owners   int64  `json:"owners"`
	Equipped int64  `json:"equipped"`
}

type SourceStat struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

type ItemStat struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Slot     string `json:"slot"`
	Rarity   string `json:"rarity"`
	Owners   int64  `json:"owners"`
	Equipped int64  `json:"equipped"`
}

type DailyGrantRow struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}
