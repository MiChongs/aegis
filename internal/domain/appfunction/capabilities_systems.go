package appfunction

import "slices"

// 平台各子系统的能力。与 capabilities.go 是同一张目录（init 里合并并按分组排好），
// 拆文件只是因为它们太多：权益、成长、玩法、交易、内容、工单、云存储、设备、函数互调。
//
// 每一项都落到那个系统**面向终端用户的同一个入口**（签到走签到服务、抽奖走抽奖服务……），
// 因此风控、幂等、通知、排行榜口径与用户自己在客户端上操作完全一致 ——
// 脚本只是替这个用户按了一次按钮，不是绕到账本背后改数字。
const (
	CapPointsRead      = "points.read"
	CapLevelRead       = "level.read"
	CapLevelWrite      = "level.write"
	CapRightsRead      = "rights.read"
	CapRightsGrant     = "rights.grant"
	CapRightsConsume   = "rights.consume"
	CapSignInRead      = "signin.read"
	CapSignInWrite     = "signin.write"
	CapInviteRead      = "invite.read"
	CapLeaderboardRead = "leaderboard.read"
	CapLotteryRead     = "lottery.read"
	CapLotteryDraw     = "lottery.draw"
	CapStoreRead       = "store.read"
	CapStoreRedeem     = "store.redeem"
	CapCardKeyRedeem   = "cardkey.redeem"
	CapCosmeticRead    = "cosmetic.read"
	CapCosmeticWrite   = "cosmetic.write"
	CapNumberRead      = "number.read"
	CapPaymentRead     = "payment.read"
	CapCloudRead       = "cloud.read"
	CapCloudWrite      = "cloud.write"
	CapTicketRead      = "ticket.read"
	CapTicketWrite     = "ticket.write"
	CapContentRead     = "content.read"
	CapDeviceRead      = "device.read"
	CapFunctionCall    = "function.call"
)

// 新增的分组。
const (
	CapGroupRights  = "rights"
	CapGroupGrowth  = "growth"
	CapGroupEngage  = "engage"
	CapGroupCompose = "compose"
)

// CapabilityGroup 一个能力分组的自述（控制台分节标题与图标键）。
type CapabilityGroup struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// capabilityGroups 顺序即控制台展示顺序。
var capabilityGroups = []CapabilityGroup{
	{Key: CapGroupIdentity, Label: "身份与账号", Hint: "读用户状态、处置账号、看他的设备"},
	{Key: CapGroupAsset, Label: "资产", Hint: "积分、会员、钱包：每一笔都进各自的正式账本"},
	{Key: CapGroupRights, Label: "权益", Hint: "功能解锁、功能时长、免广告、抽奖次数、装扮……全站能发的东西都从这里发"},
	{Key: CapGroupGrowth, Label: "成长", Hint: "等级与经验、签到、邀请、排行榜"},
	{Key: CapGroupEngage, Label: "玩法与交易", Hint: "抽奖、积分商城、卡密、装扮、号码、订单：替用户走一遍客户端上的同一个入口"},
	{Key: CapGroupState, Label: "状态", Hint: "服务端独占的键值、用户云存储、分布式锁"},
	{Key: CapGroupReach, Label: "触达", Hint: "站内信、实时推送、邮件、工单"},
	{Key: CapGroupIntel, Label: "情报与内容", Hint: "IP 归属地、应用公告与横幅"},
	{Key: CapGroupAudit, Label: "审计", Hint: "写平台审计日志"},
	{Key: CapGroupEgress, Label: "出站", Hint: "HTTPS 请求与 AI 生成"},
	{Key: CapGroupCompose, Label: "编排", Hint: "调用同一应用的其他函数"},
	{Key: CapGroupLegacy, Label: "旧能力", Hint: "只为存量数据保留"},
}

// CapabilityGroups 能力分组（副本）。
func CapabilityGroups() []CapabilityGroup { return slices.Clone(capabilityGroups) }

var systemCapabilities = []Capability{
	// ── 资产 ──
	{
		Key: CapPointsRead, Group: CapGroupAsset,
		Label: "读取积分", API: "aegis.points.balance / history",
		Hint:      "余额与最近的积分 / 经验流水（与客户端积分页同一份数据）",
		Risk:      RiskLow,
		Namespace: "points",
		Members:   []string{"balance", "history"},
		Declaration: `
    /** 积分余额。省略 userId 时读当前调用者 */
    balance(userId?: number): number;
    /** 最近的流水，kind 缺省 integral（积分），experience 为经验；limit 上限 50 */
    history(options?: { kind?: "integral" | "experience"; limit?: number }): AegisLedgerEntry[];`,
		Interfaces: `
declare interface AegisLedgerEntry {
  transactionNo: string;
  /** earn / spend / adjust … */
  type: string;
  category: string;
  amount: number;
  balanceBefore: number;
  balanceAfter: number;
  title: string;
  description: string;
  createdAt: string;
}`,
	},

	// ── 权益 ──
	{
		Key: CapRightsRead, Group: CapGroupRights,
		Label: "读取权益", API: "aegis.rights.summary()",
		Hint:      "会员、单独解锁的功能、免广告、抽奖次数、功能时长 —— 一次说清这个人手里有什么",
		Risk:      RiskLow,
		Namespace: "rights",
		Members:   []string{"summary", "has", "featureTime", "catalog"},
		Declaration: `
    /** 权益总览。省略 userId 时读当前调用者 */
    summary(userId?: number): AegisRights | null;
    /** 是否能用某个功能：会员套餐带的、单独解锁的都算 */
    has(feature: string, userId?: number): boolean;
    /** 某个功能的计时账户（按秒计量的功能时长） */
    featureTime(feature: string, userId?: number): AegisFeatureTime;
    /** rights.grant 能发的全部物品类型及取值范围 */
    catalog(): AegisGrantItemSpec[];`,
		Interfaces: `
declare interface AegisRights {
  userId: number;
  vip: boolean;
  vipLifetime: boolean;
  vipExpireAt?: string;
  /** 当前能用的全部功能标识（会员套餐带的 + 单独解锁的） */
  features: string[];
  /** 单独解锁的功能 */
  unlocks: { tag: string; permanent: boolean; expireAt?: string }[];
  adFree: boolean;
  adFreeUntil?: string;
  /** 抽奖次数余额（应用级，任意活动可用） */
  lotteryTickets: number;
}

declare interface AegisFeatureTime {
  feature: string;
  name: string;
  /** 剩余秒数 */
  balanceSeconds: number;
  /** 会员或免广告期内免扣 */
  exempt: boolean;
  earnedSeconds: number;
  usedSeconds: number;
}

declare interface AegisGrantItemSpec {
  type: AegisGrantItemType;
  label: string;
  hint: string;
  group: string;
  unit?: string;
  min?: number;
  max?: number;
}

declare type AegisGrantItemType =
  | "points" | "experience" | "vip_days" | "vip_plan" | "vip_lifetime"
  | "feature" | "feature_time" | "ad_free" | "lottery_draws" | "ai_credit"
  | "balance" | "license_days" | "device_slots"
  | "number_change" | "number_ticket" | "number_draw" | "number_assign" | "cosmetic";

declare interface AegisGrantItem {
  type: AegisGrantItemType;
  /** 数量：积分、经验、天数、分钟、次数、张数 */
  amount?: number;
  /** 钱包余额，字符串以免丢精度 */
  money?: string;
  /** 会员套餐编号 / 装扮编号 / 靓号券等级（1–4） */
  refId?: number;
  /** 功能标识（feature / feature_time） */
  tag?: string;
  /** 指定号码（number_assign） */
  text?: string;
}

declare interface AegisGrantResult {
  grantNo: string;
  /** 同一个 key 已经发过，这次直接回第一次的结果 */
  replayed: boolean;
  simulated?: boolean;
  results: { type: string; label: string; detail: string; expireAt?: string; converted?: boolean }[];
}`,
	},
	{
		Key: CapRightsGrant, Group: CapGroupRights,
		Label: "发放权益", API: "aegis.rights.grant(items, options)",
		Hint: "复用物品发放引擎的同一个事务：会员、功能解锁、功能时长、免广告、抽奖次数、AI 额度、余额、号码、装扮全部可发，带幂等键",
		Risk: RiskHigh, Mutating: true, RequiresUser: true,
		Namespace: "rights",
		Members:   []string{"grant"},
		Declaration: `
    /**
     * 给当前调用者发一组物品（整笔原子：任何一项发不出去全部回滚）。
     * key 是幂等键：同一个函数里、同一个人、同一个 key 只发一次，缺省按 eventId 生成 —— 重试不会多发。
     */
    grant(items: AegisGrantItem | AegisGrantItem[], options?: { key?: string; title?: string }): AegisGrantResult;`,
	},
	{
		Key: CapRightsConsume, Group: CapGroupRights,
		Label: "扣减功能时长", API: "aegis.rights.consumeTime(feature, seconds)",
		Hint: "从功能计时账户扣秒数（与客户端计时同一个账户）；会员与免广告期内免扣",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "rights",
		Members:   []string{"consumeTime"},
		Declaration: `
    /** 扣当前调用者某个功能的时长。余额不足抛错（40328）；免扣时 exempt 为 true 且不扣 */
    consumeTime(feature: string, seconds: number): { exempt: boolean; seconds: number; balanceSeconds: number };`,
	},

	// ── 成长 ──
	{
		Key: CapLevelRead, Group: CapGroupGrowth,
		Label: "读取等级", API: "aegis.level.get()",
		Hint:      "当前等级、经验、距下一级还差多少",
		Risk:      RiskLow,
		Namespace: "level",
		Members:   []string{"get"},
		Declaration: `
    /** 等级与经验进度。省略 userId 时读当前调用者 */
    get(userId?: number): AegisLevel | null;`,
		Interfaces: `
declare interface AegisLevel {
  level: number;
  levelName: string;
  experience: number;
  expInLevel: number;
  expToNext: number;
  /** 0~1 */
  progress: number;
  nextLevel: number;
  nextLevelName: string;
  isMax: boolean;
  maxLevel: number;
  /** 经验倍率 */
  multiplier: number;
}`,
	},
	{
		Key: CapLevelWrite, Group: CapGroupGrowth,
		Label: "增加经验", API: "aegis.level.addExperience(amount)",
		Hint: "进经验账本并同步等级（升级奖励照常发放）",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "level",
		Members:   []string{"addExperience"},
		Declaration: `
    /** 给当前调用者加经验，返回变更后的经验与等级 */
    addExperience(amount: number, reason?: string): { experience: number; level: number; levelChanged: boolean };`,
	},
	{
		Key: CapSignInRead, Group: CapGroupGrowth,
		Label: "读取签到", API: "aegis.signin.status()",
		Hint:      "今天签了没有、连签几天",
		Risk:      RiskLow,
		Namespace: "signin",
		Members:   []string{"status"},
		Declaration: `
    /** 当前调用者的签到状态 */
    status(): AegisSignInStatus;`,
		Interfaces: `
declare interface AegisSignInStatus {
  todaySigned: boolean;
  consecutiveDays: number;
  totalSignIns: number;
  lastSignedDate?: string;
  [key: string]: any;
}`,
	},
	{
		Key: CapSignInWrite, Group: CapGroupGrowth,
		Label: "代为签到", API: "aegis.signin.checkIn()",
		Hint: "替当前调用者签到：奖励、连签、发放规则的「每日签到」事件照常生效",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "signin",
		Members:   []string{"checkIn"},
		Declaration: `
    /** 替当前调用者签到。今天已签过时 alreadySigned 为 true，不重复发奖励 */
    checkIn(): { alreadySigned: boolean; consecutiveDays: number; integral: number; experience: number };`,
	},
	{
		Key: CapInviteRead, Group: CapGroupGrowth,
		Label: "读取邀请", API: "aegis.invite.center()",
		Hint:      "邀请码、邀请了几人、被谁邀请",
		Risk:      RiskLow,
		Namespace: "invite",
		Members:   []string{"center"},
		Declaration: `
    /** 当前调用者的邀请中心 */
    center(): AegisInviteCenter;`,
		Interfaces: `
declare interface AegisInviteCenter {
  enabled: boolean;
  code: string;
  canInvite: boolean;
  stats: { invited: number; rewarded: number; pending: number; todayRewarded: number };
  /** 邀请他的人（没有为 null） */
  inviter: { userId: number; code: string; nickname?: string } | null;
  [key: string]: any;
}`,
	},
	{
		Key: CapLeaderboardRead, Group: CapGroupGrowth,
		Label: "读取排行榜", API: "aegis.leaderboard.board(key)",
		Hint:      "某个榜的前 N 名与调用者自己的名次",
		Risk:      RiskLow,
		Namespace: "leaderboard",
		Members:   []string{"boards", "board"},
		Declaration: `
    /** 应用开放的全部榜单 */
    boards(): any[];
    /** 某个榜（points_total / points_daily / points_weekly / points_monthly / experience / level …）；limit 上限 100 */
    board(key: string, limit?: number): AegisBoard;`,
		Interfaces: `
declare interface AegisBoard {
  total: number;
  items: { rank: number; name: string; avatar?: string; value: number; isMe: boolean }[];
  me: { rank?: number; value: number; gapToPrev?: number; inTop: boolean };
  [key: string]: any;
}`,
	},

	// ── 玩法与交易 ──
	{
		Key: CapLotteryRead, Group: CapGroupEngage,
		Label: "读取抽奖", API: "aegis.lottery.activities / state / tickets",
		Hint:      "进行中的活动、某个活动的奖池与剩余机会、抽奖次数余额",
		Risk:      RiskLow,
		Namespace: "lottery",
		Members:   []string{"activities", "state", "tickets"},
		Declaration: `
    /** 进行中的抽奖活动 */
    activities(): { id: number; name: string; description: string; startTime?: string; endTime?: string }[];
    /** 某个活动对当前调用者的状态（奖池、剩余机会、保底进度） */
    state(activityId: number): any;
    /** 抽奖次数账户 */
    tickets(): { balance: number; gift: number; purchased: number; totalGranted: number; totalConsumed: number };`,
	},
	{
		Key: CapLotteryDraw, Group: CapGroupEngage,
		Label: "代为抽奖", API: "aegis.lottery.draw(activityId)",
		Hint: "走抽奖服务的同一个入口：机会扣减、风控、保底、奖品发放、通知全部照常",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "lottery",
		Members:   []string{"draw"},
		Declaration: `
    /** 替当前调用者抽奖。source 缺省自动选（免费 → 次数 → 积分）；count 为连抽次数（1–10） */
    draw(activityId: number, options?: { source?: "free" | "bonus" | "points"; count?: number }): AegisDrawResult;`,
		Interfaces: `
declare interface AegisDrawResult {
  draws: { drawNo: string; prizeId: number; prizeName: string; prizeKind: string; rewardType: string; status: string }[];
  replayed: boolean;
  simulated?: boolean;
  [key: string]: any;
}`,
	},
	{
		Key: CapStoreRead, Group: CapGroupEngage,
		Label: "读取积分商城", API: "aegis.store.items()",
		Hint:      "商品、价格、库存与调用者当前能不能换",
		Risk:      RiskLow,
		Namespace: "store",
		Members:   []string{"items"},
		Declaration: `
    /** 积分商城（对当前调用者的视图：价格、库存、限购、此刻能否兑换） */
    items(): { enabled: boolean; balance: number; items: any[] };`,
	},
	{
		Key: CapStoreRedeem, Group: CapGroupEngage,
		Label: "代为兑换", API: "aegis.store.redeem(itemId)",
		Hint: "扣积分换商品：库存、限购、风控与发货全部走商城的同一个事务",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "store",
		Members:   []string{"redeem"},
		Declaration: `
    /** 替当前调用者兑换一件商品。同一次调用里对同一商品重复调用按次计（不会被当成重放） */
    redeem(itemId: number): { redemptionNo: string; itemTitle: string; cost: number; status: string; balance: number; replayed: boolean; simulated?: boolean };`,
	},
	{
		Key: CapCardKeyRedeem, Group: CapGroupEngage,
		Label: "代为兑换卡密", API: "aegis.cardkey.redeem(code)",
		Hint: "替调用者兑换一张卡密，防爆破与风控照常生效",
		Risk: RiskHigh, Mutating: true, RequiresUser: true,
		Namespace: "cardkey",
		Members:   []string{"redeem"},
		Declaration: `
    /** 替当前调用者兑换卡密 */
    redeem(code: string): { code: string; kind: string; batchName: string; results: any[]; expiresAt?: string; simulated?: boolean };`,
	},
	{
		Key: CapCosmeticRead, Group: CapGroupEngage,
		Label: "读取装扮", API: "aegis.cosmetic.owned / loadout",
		Hint:      "拥有哪些头像框、资料卡、昵称样式、勋章，当前戴着哪些",
		Risk:      RiskLow,
		Namespace: "cosmetic",
		Members:   []string{"owned", "loadout"},
		Declaration: `
    /** 装扮中心：全部装扮与当前调用者的拥有、到期、佩戴状态 */
    owned(): any;
    /** 当前佩戴 */
    loadout(): { avatarFrame?: any; profileCard?: any; nameStyle?: any; badges: any[] };`,
	},
	{
		Key: CapCosmeticWrite, Group: CapGroupEngage,
		Label: "佩戴装扮", API: "aegis.cosmetic.equip(slot, id)",
		Hint: "替调用者佩戴或卸下装扮（只能戴拥有的）",
		Risk: RiskLow, Mutating: true, RequiresUser: true,
		Namespace: "cosmetic",
		Members:   []string{"equip"},
		Declaration: `
    /** 佩戴装扮；cosmeticId 传 0 卸下。slot：avatar_frame / profile_card / name_style / badge */
    equip(slot: string, cosmeticId: number, position?: number): any;`,
	},
	{
		Key: CapNumberRead, Group: CapGroupEngage,
		Label: "读取号码", API: "aegis.number.get()",
		Hint:      "调用者当前的应用号码与靓号等级",
		Risk:      RiskLow,
		Namespace: "number",
		Members:   []string{"get"},
		Declaration: `
    /** 当前调用者的号码。未开启号码时 enabled 为 false */
    get(userId?: number): { enabled: boolean; number: number; tier: number; tierName: string; [key: string]: any } | null;`,
	},
	{
		Key: CapPaymentRead, Group: CapGroupEngage,
		Label: "读取订单", API: "aegis.payment.orders / order",
		Hint:         "调用者自己的支付订单（金额是字符串）",
		Risk:         RiskLow,
		RequiresUser: true,
		Namespace:    "payment",
		Members:      []string{"orders", "order"},
		Declaration: `
    /** 当前调用者的订单，按创建时间倒序；limit 上限 50 */
    orders(options?: { status?: string; limit?: number }): AegisOrder[];
    /** 按订单号查当前调用者的一笔订单，不是他的返回 null */
    order(orderNo: string): AegisOrder | null;`,
		Interfaces: `
declare interface AegisOrder {
  orderNo: string;
  subject: string;
  /** 定点小数字符串 */
  amount: string;
  currency: string;
  paymentMethod: string;
  status: string;
  refundedAmount: string;
  paidAt?: string;
  createdAt: string;
}`,
	},

	// ── 状态 ──
	{
		Key: CapCloudRead, Group: CapGroupState,
		Label: "读取云存储", API: "aegis.cloud.get(namespace, key)",
		Hint:         "调用者的用户云存储条目（客户端同步上来的数据）",
		Risk:         RiskLow,
		RequiresUser: true,
		Namespace:    "cloud",
		Members:      []string{"get"},
		Declaration: `
    /** 读当前调用者的一条云存储；不存在返回 null。JSON 内容直接是对象，文本是字符串 */
    get(namespace: string, key: string): { revision: number; contentType: string; content: any; updatedAt: string } | null;`,
	},
	{
		Key: CapCloudWrite, Group: CapGroupState,
		Label: "写入云存储", API: "aegis.cloud.put(namespace, key, value)",
		Hint: "写调用者的云存储条目，客户端下次同步就能拿到；支持乐观锁",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "cloud",
		Members:   []string{"put"},
		Declaration: `
    /** 写当前调用者的一条云存储。对象按 JSON 存，字符串按文本存；ifRevision 不匹配时抛错 */
    put(namespace: string, key: string, value: any, options?: { ifRevision?: number }): { revision: number };`,
	},

	// ── 触达 ──
	{
		Key: CapTicketRead, Group: CapGroupReach,
		Label: "读取工单", API: "aegis.tickets.list()",
		Hint:         "调用者自己提过的工单",
		Risk:         RiskLow,
		RequiresUser: true,
		Namespace:    "tickets",
		Members:      []string{"list"},
		Declaration: `
    /** 当前调用者的工单，limit 上限 50 */
    list(options?: { status?: string; limit?: number }): { id: number; ticketNo: string; title: string; status: string; priority: string; createdAt: string; [key: string]: any }[];`,
	},
	{
		Key: CapTicketWrite, Group: CapGroupReach,
		Label: "代为提单", API: "aegis.tickets.create()",
		Hint: "以调用者身份提一张工单，客服后台照常收到提醒",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "tickets",
		Members:   []string{"create"},
		Declaration: `
    /** 以当前调用者身份提工单 */
    create(ticket: { title: string; content: string; priority?: "low" | "normal" | "high"; categoryId?: number }): { id: number; ticketNo: string; status: string; simulated?: boolean };`,
	},

	// ── 情报与内容 ──
	{
		Key: CapContentRead, Group: CapGroupIntel,
		Label: "读取应用内容", API: "aegis.content.banners / notices",
		Hint:      "应用当前生效的横幅与公告",
		Risk:      RiskLow,
		Namespace: "content",
		Members:   []string{"banners", "notices"},
		Declaration: `
    /** 生效中的横幅 */
    banners(): { id: number; title: string; content: string; url: string; type: string; position: number; [key: string]: any }[];
    /** 生效中的公告 */
    notices(): { id: number; title: string; summary: string; content: string; level: string; pinned: boolean; [key: string]: any }[];`,
	},

	// ── 身份 ──
	{
		Key: CapDeviceRead, Group: CapGroupIdentity,
		Label: "读取设备", API: "aegis.devices.list()",
		Hint:         "调用者登录过的设备与设备数上限（不含 IP）",
		Risk:         RiskLow,
		RequiresUser: true,
		Namespace:    "devices",
		Members:      []string{"list"},
		Declaration: `
    /** 当前调用者的设备与上限 */
    list(): { enabled: boolean; limits: any; devices: { deviceId: string; platform: string; name: string; lastActiveAt?: string; current: boolean; [key: string]: any }[] };`,
	},

	// ── 编排 ──
	{
		Key: CapFunctionCall, Group: CapGroupCompose,
		Label: "调用其他函数", API: "aegis.functions.call(name, input)",
		Hint: "同步调用本应用的另一个已激活函数，身份不变；最多嵌套 3 层，试跑时被调函数同样只记不做",
		Risk: RiskMedium, Mutating: true,
		Namespace: "functions",
		Members:   []string{"call"},
		Declaration: `
    /** 调用本应用另一个函数，返回它的输出。被调函数抛的业务错误原样抛出 */
    call(name: string, input?: Record<string, any>): any;`,
	},
}

// groupRank 分组在控制台上的位置。
func groupRank(group string) int {
	for i, item := range capabilityGroups {
		if item.Key == group {
			return i
		}
	}
	return len(capabilityGroups)
}

func init() {
	capabilities = append(capabilities, systemCapabilities...)
	// 按分组稳定排序：组内保持声明顺序（读在写前、低危在高危前）
	slices.SortStableFunc(capabilities, func(a, b Capability) int {
		return groupRank(a.Group) - groupRank(b.Group)
	})
}
