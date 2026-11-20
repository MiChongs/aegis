// Package usernumber 用户号码（docs/user-number.md）：每个应用一套、可以更换的对外号码。
//
// ── 两层身份 ──
//
//	对外 ID（users.public_id）  16 位随机数、终生不变：OAuth2 sub、头像与广告令牌、客户端本地数据都认它
//	号码（users.number）        5–12 位纯数字、按应用唯一、可以更换：用户看到、搜索、炫耀的就是它
//
// 号码只是「展示与查找用的名字」，任何令牌、签名、第三方关联都不挂在号码上：换号不会让任何东西失效。
//
// ── 等级 ──
//
// 号码按数字规律自动分五级（普通 / 精品 / 稀有 / 传说 / 至尊），规律目录在 patterns.go，
// 每条规律落在哪一级可以按应用调整（也可以停用）。一个号码命中多条规律时取最高的一级。
// 新用户与存量回填拿到的都是「默认位数、不命中任何规律」的普通号（数据库函数按同一套规律的超集过滤）。
//
// ── 权益 ──
//
//	改号卡（change）    自选一个普通号
//	靓号券（ticket）    带等级 1–4：自选不高于该级的号（也能用来换普通号）
//	摇号次数（draw）    摇一次给 N 个候选号、锁定若干分钟，任选其一直接换上（不再扣别的权益）；
//	                    各级出现的概率按应用的权重算，非普通级的权重乘以今日号运（每日运气 × 至尊加成）
//
// 权益是计数账户（user_number_rights）+ 流水（user_number_ledger），由全站各发放渠道
// （物品发放、等级、卡密、积分商城、抽奖、邀请、排行榜、推广、激励广告）在各自事务里发放。
// 后台还能直接给用户指定一个号码（含保留号），物品发放另有「指定号码」一项。
//
// ── 号码的占用 ──
//
// 一个号码同一时刻只属于一处：某个用户正在用（users.number）、或者被占着（user_number_holds）：
//
//	reserved   后台保留，用户自选与摇号都拿不到，只能由后台指定
//	frozen     刚被换下来的旧号，冻结期内只有原主人能换回去
//	candidate  摇号的候选号，锁定期内只有摇出它的人能选
package usernumber

import "time"

// 等级，由低到高。数值即等级，靓号券的 level 与它对齐。
const (
	TierCommon  = 0
	TierFine    = 1
	TierRare    = 2
	TierLegend  = 3
	TierSupreme = 4

	TierCount = 5
)

// TierSpec 一级的展示信息。
type TierSpec struct {
	Level int    `json:"level"`
	Key   string `json:"key"`
	Name  string `json:"name"`
	// Color 建议的主色（#RRGGBB），客户端与控制台的徽章用它
	Color string `json:"color"`
}

var tierSpecs = [TierCount]TierSpec{
	{Level: TierCommon, Key: "common", Name: "普通", Color: "#7A8594"},
	{Level: TierFine, Key: "fine", Name: "精品", Color: "#2F9E6E"},
	{Level: TierRare, Key: "rare", Name: "稀有", Color: "#2F6FDE"},
	{Level: TierLegend, Key: "legend", Name: "传说", Color: "#8B4BDB"},
	{Level: TierSupreme, Key: "supreme", Name: "至尊", Color: "#D4A017"},
}

// Tiers 五级的展示信息，由低到高。
func Tiers() []TierSpec { return append([]TierSpec(nil), tierSpecs[:]...) }

// TierOf 一级的展示信息；越界时夹到范围内。
func TierOf(level int) TierSpec { return tierSpecs[clampTier(level)] }

// ValidTier 是否合法的等级。
func ValidTier(level int) bool { return level >= TierCommon && level <= TierSupreme }

func clampTier(level int) int {
	if level < TierCommon {
		return TierCommon
	}
	if level > TierSupreme {
		return TierSupreme
	}
	return level
}

// 权益种类。
const (
	RightChange = "change"
	RightTicket = "ticket"
	RightDraw   = "draw"
)

// ValidRight 种类与等级是否成对：改号卡与摇号次数的 level 恒为 0，靓号券 1–4。
func ValidRight(kind string, level int) bool {
	switch kind {
	case RightChange, RightDraw:
		return level == 0
	case RightTicket:
		return level >= TierFine && level <= TierSupreme
	}
	return false
}

// RightName 权益的中文名（靓号券带等级）。
func RightName(kind string, level int) string {
	switch kind {
	case RightChange:
		return "改号卡"
	case RightDraw:
		return "摇号次数"
	case RightTicket:
		return TierOf(level).Name + "靓号券"
	}
	return kind
}

// 流水来源（发放渠道）。
const (
	OriginItemGrant   = "item_grant"
	OriginLevel       = "level"
	OriginCardKey     = "card_key"
	OriginPointsStore = "points_store"
	OriginLottery     = "lottery"
	OriginInvite      = "invite"
	OriginLeaderboard = "leaderboard"
	OriginPromo       = "promo"
	OriginRewardedAd  = "rewarded_ad"
	OriginAdmin       = "admin"
	// OriginChange 换号用掉 / 摇号用掉
	OriginChange = "change"
	OriginDraw   = "draw"
	// OriginRefund 换号失败退回（事务回滚之外的情形，例如摇号单过期前主动放弃不退，这里留作人工退回）
	OriginRefund = "refund"
)

var originLabels = map[string]string{
	OriginItemGrant:   "物品发放",
	OriginLevel:       "等级奖励",
	OriginCardKey:     "卡密兑换",
	OriginPointsStore: "积分商城",
	OriginLottery:     "抽奖",
	OriginInvite:      "邀请奖励",
	OriginLeaderboard: "排行榜奖励",
	OriginPromo:       "推广活动",
	OriginRewardedAd:  "激励广告",
	OriginAdmin:       "管理员调整",
	OriginChange:      "更换号码",
	OriginDraw:        "摇号",
	OriginRefund:      "退回",
}

// OriginLabel 来源的中文名；不认识的原样返回。
func OriginLabel(origin string) string {
	if label, ok := originLabels[origin]; ok {
		return label
	}
	return origin
}

// 换号的方式（历史记录的 source）。
const (
	SourceInitial  = "initial"  // 建号或回填时发的号（不进历史，只在说明里用）
	SourceChange   = "change"   // 用改号卡
	SourceTicket   = "ticket"   // 用靓号券
	SourceDraw     = "draw"     // 摇号选中
	SourceFrozen   = "frozen"   // 换回自己冻结中的旧号（不扣权益）
	SourceAdmin    = "admin"    // 管理员指定
	SourceGrant    = "grant"    // 物品发放「指定号码」
	SourceRollback = "rollback" // 管理员回滚到某次之前的号
)

// 占用种类。
const (
	HoldReserved  = "reserved"
	HoldFrozen    = "frozen"
	HoldCandidate = "candidate"
)

// ValidHoldKind 是否合法的占用种类。
func ValidHoldKind(kind string) bool {
	return kind == HoldReserved || kind == HoldFrozen || kind == HoldCandidate
}

// 摇号单状态。
const (
	DrawOpen      = "open"
	DrawPicked    = "picked"
	DrawDiscarded = "discarded"
	DrawExpired   = "expired"
)

// ── 读模型 ──

// Right 一种权益的余额。
type Right struct {
	Kind    string `json:"kind"`
	Level   int    `json:"level"`
	Name    string `json:"name"`
	Balance int64  `json:"balance"`
	Earned  int64  `json:"earned"`
	Used    int64  `json:"used"`
}

// Rights 一个人的全部权益，按 改号卡、靓号券（由低到高）、摇号次数 排。没有的种类余额为 0。
type Rights struct {
	Change  int64   `json:"change"`
	Draw    int64   `json:"draw"`
	Tickets []Right `json:"tickets"`
	Items   []Right `json:"items"`
}

// Candidate 摇号的一个候选号。
type Candidate struct {
	Number   int64    `json:"number"`
	Tier     int      `json:"tier"`
	Patterns []string `json:"patterns,omitempty"`
}

// Draw 一张摇号单。
type Draw struct {
	ID         int64       `json:"id"`
	AppID      int64       `json:"appid"`
	UserID     int64       `json:"userId"`
	Candidates []Candidate `json:"candidates"`
	// Luck 摇号时的号运系数（1 为平均）
	Luck      float64    `json:"luck"`
	Status    string     `json:"status"`
	Picked    *int64     `json:"picked,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	ClosedAt  *time.Time `json:"closedAt,omitempty"`
}

// History 一次换号。
type History struct {
	ID        int64     `json:"id"`
	AppID     int64     `json:"appid"`
	UserID    int64     `json:"userId"`
	OldNumber *int64    `json:"oldNumber,omitempty"`
	NewNumber int64     `json:"newNumber"`
	Tier      int       `json:"tier"`
	Source    string    `json:"source"`
	RightKind string    `json:"rightKind,omitempty"`
	RightLvl  int       `json:"rightLevel"`
	DrawID    *int64    `json:"drawId,omitempty"`
	Operator  string    `json:"operator,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// 管理端列表附带
	Account  string `json:"account,omitempty"`
	Nickname string `json:"nickname,omitempty"`
}

// LedgerEntry 一条权益流水。
type LedgerEntry struct {
	ID           int64     `json:"id"`
	AppID        int64     `json:"appid"`
	UserID       int64     `json:"userId"`
	Kind         string    `json:"kind"`
	Level        int       `json:"level"`
	Name         string    `json:"name"`
	Delta        int64     `json:"delta"`
	BalanceAfter int64     `json:"balanceAfter"`
	Origin       string    `json:"origin"`
	OriginLabel  string    `json:"originLabel"`
	Ref          string    `json:"ref,omitempty"`
	Title        string    `json:"title,omitempty"`
	Operator     string    `json:"operator,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	Account      string    `json:"account,omitempty"`
	Nickname     string    `json:"nickname,omitempty"`
}

// Hold 一条占用。
type Hold struct {
	AppID     int64      `json:"appid"`
	Number    int64      `json:"number"`
	Kind      string     `json:"kind"`
	Tier      int        `json:"tier"`
	UserID    *int64     `json:"userId,omitempty"`
	DrawID    *int64     `json:"drawId,omitempty"`
	Note      string     `json:"note,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedBy string     `json:"createdBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// Grant 发一份权益（各渠道在自己的事务里调用仓储的 grantUserNumberRightTx）。
type Grant struct {
	AppID    int64
	UserID   int64
	Kind     string
	Level    int
	Amount   int64
	Origin   string
	Ref      string
	Title    string
	Operator string
}

// ChangeRequest 一次换号。
type ChangeRequest struct {
	AppID  int64
	UserID int64
	Number int64
	// DrawID 从摇号单里选：号码必须是这张单的候选号，不再扣别的权益
	DrawID int64
	// UseKind / UseLevel 指定用哪一种权益；留空时自动选能用的里最便宜的（普通号先用改号卡）
	UseKind  string
	UseLevel int
}

// ChangeResult 换号结果。
type ChangeResult struct {
	Number    int64    `json:"number"`
	Tier      int      `json:"tier"`
	OldNumber *int64   `json:"oldNumber,omitempty"`
	Source    string   `json:"source"`
	Used      *Right   `json:"used,omitempty"`
	Rights    Rights   `json:"rights"`
	Patterns  []string `json:"patterns,omitempty"`
	// NextChangeAt 下一次可以换号的时间（没有冷却时为空）
	NextChangeAt *time.Time `json:"nextChangeAt,omitempty"`
}

// AssignRequest 管理员或物品发放直接指定号码。
type AssignRequest struct {
	AppID  int64
	UserID int64
	Number int64
	// Force 管理员：可以拿保留号与别人的冻结号、不看冷却（仍不能抢别人正在用的号）
	Force    bool
	Source   string
	Operator string
	Note     string
}

// Overview 管理端总览。
type Overview struct {
	Users        int64            `json:"users"`
	Numbered     int64            `json:"numbered"`
	ByTier       []TierTally      `json:"byTier"`
	ChangesToday int64            `json:"changesToday"`
	Changes7d    int64            `json:"changes7d"`
	DrawsToday   int64            `json:"drawsToday"`
	OpenDraws    int64            `json:"openDraws"`
	Holds        map[string]int64 `json:"holds"`
	Outstanding  []Right          `json:"outstanding"`
}

// TierTally 一级的号码数。
type TierTally struct {
	Tier  int   `json:"tier"`
	Count int64 `json:"count"`
}
