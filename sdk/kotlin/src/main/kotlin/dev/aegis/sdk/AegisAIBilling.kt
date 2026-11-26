package dev.aegis.sdk

import kotlinx.serialization.Serializable

/*
 * AI 中转网关用户端的数据类（服务端 docs/ai-billing.md「用户端」与 docs/ai-gateway.md）。
 *
 * 2026-11-26 起 AI 额度是独立资产，与积分不互通：来源是每日免费额度、会员每日 / 每月额度、额度包（有到期时间）、
 * 永久额度（广告、活动、退款），以及钱包余额直付。
 *
 * 约定：
 * - 额度（点）与金额（元）、倍率一律是十进制字符串，与钱包同一惯例；客户端只展示，不做会丢精度的计算。
 * - 时间是 RFC 3339 字符串（带时区偏移），没有时为 null。
 * - 每个字段都有默认值：旧版服务端缺字段、或者字段为 null 时（经 [AegisAIGatewayApi] 解码会把 null 换成默认值）不崩。
 */

/** `GET /ai/overview`：开放状态、扣费顺序、各项额度、倍率、今日用量与调用地址。 */
@Serializable
data class AegisAIOverview(
    val enabled: Boolean = false,
    val allowSessionToken: Boolean = true,
    /** `all` / `vip`。 */
    val audience: String = "all",
    /** 当前用户能否使用（仅会员开放时，非会员为 false）。 */
    val eligible: Boolean = true,
    /** 额度的称呼，服务端统一为「额度」。 */
    val unit: String = "",
    /** 启用的扣费来源，顺序即扣费顺序。 */
    val sources: List<AegisAISource> = emptyList(),
    val profile: AegisAIProfile = AegisAIProfile(),
    /** 此刻生效的会员优惠；非会员时 source 为 `none`，各项为中性值。 */
    val member: AegisAIMemberBenefit = AegisAIMemberBenefit(),
    /** 此刻对全部模型的合计倍率（= [pricing] 的 total）。 */
    val factor: String = "1",
    /** 倍率分解。 */
    val pricing: AegisAIFactorBreakdown = AegisAIFactorBreakdown(),
    /** 下一次价格变化的时刻；一周内不变时为 null。 */
    val nextChangeAt: String? = null,
    /** 周期额度（每日免费、会员每日、会员每月）的当期用量。 */
    val quotas: List<AegisAIQuota> = emptyList(),
    /** 额度包可用 / 调用暂扣中。 */
    val pack: String = "0",
    val packFrozen: String = "0",
    /** 永久额度可用 / 调用暂扣中。 */
    val credit: String = "0",
    val creditFrozen: String = "0",
    /** AI 额度合计：额度包 + 永久额度（不含暂扣中）。 */
    val balance: String = "0",
    /** 7 天内到期的额度包合计与最早的到期时间；没有时为 null。 */
    val expiring: AegisAIExpiring? = null,
    /** 钱包余额（元）。 */
    val wallet: String = "0",
    /** 余额直付：每 1 额度扣多少元。 */
    val walletRate: String = "0",
    /** 现在一共能用多少额度（启用的来源合计，含余额折算，扣掉暂扣中的部分）。 */
    val available: String = "0",
    /** 每人每天最多消费多少额度，"0" 表示不限。 */
    val dailySpendLimit: String = "0",
    val todaySpent: String = "0",
    val totalSpent: String = "0",
    val totalRequests: Long = 0,
    /** 会员档位生效后的每分钟调用数与并发上限。 */
    val rpm: Int = 0,
    val maxConcurrent: Int = 0,
    val maxKeysPerUser: Int = 0,
    val welcome: AegisAIWelcomeState = AegisAIWelcomeState(),
    val store: AegisAIStoreState = AegisAIStoreState(),
    /** OpenAI 兼容 SDK 的 baseURL（相对站点根）。 */
    val baseUrl: String = "/api/ai/v1",
) {
    fun hasSource(key: String): Boolean = sources.any { it.key == key }

    companion object {
        const val SOURCE_FREE_DAILY = "free_daily"
        const val SOURCE_VIP_DAILY = "vip_daily"
        const val SOURCE_VIP_MONTHLY = "vip_monthly"
        const val SOURCE_PACK = "pack"
        const val SOURCE_CREDIT = "credit"
        const val SOURCE_WALLET = "wallet"
    }
}

/** 一个扣费来源的自述（名称与说明由服务端给，跟着后台的配置走）。 */
@Serializable
data class AegisAISource(
    val key: String = "",
    val label: String = "",
    val hint: String = "",
    val periodic: Boolean = false,
    /** `day` / `month`，非周期来源为空。 */
    val period: String = "",
    val vipOnly: Boolean = false,
)

/** 一个周期额度的当期状态。 */
@Serializable
data class AegisAIQuota(
    val source: String = "",
    val label: String = "",
    /** 当期的标识：日额度 `2026-11-26`、月额度 `2026-11`（旧版服务端是 `day` / `month`）。 */
    val period: String = "",
    val limit: String = "0",
    val used: String = "0",
    val reserved: String = "0",
    val remaining: String = "0",
    /** 当前用户能否使用（会员额度对非会员为 false）。 */
    val eligible: Boolean = true,
)

/** 计价时用到的用户事实。 */
@Serializable
data class AegisAIProfile(
    val isVip: Boolean = false,
    val level: Int = 1,
    /** 此刻持有的会员档位。 */
    val tierIds: List<Long> = emptyList(),
    /** 计价生效的那个档位，没有为 0 / 空。 */
    val tierId: Long = 0,
    val tierName: String = "",
)

/**
 * 一档会员优惠（`MemberBenefit`）：我此刻生效的那份，或者 [AegisAIPricing.tiers] 里各档的对比。
 * 非会员时 [source] 为 [SOURCE_NONE]，倍率与折扣为 1、额度为 0。
 */
@Serializable
data class AegisAIMemberBenefit(
    /** `tier` 按档位、`vip` 通用会员配置、`none` 非会员。 */
    val source: String = SOURCE_NONE,
    val tierId: Long = 0,
    val name: String = "",
    /** 调用倍率（0.8 = 八折）。 */
    val multiplier: String = "1",
    /** 会员每日 / 每月额度。 */
    val daily: String = "0",
    val monthly: String = "0",
    /** 买额度包的折扣（0.85 = 85 折）。 */
    val packDiscount: String = "1",
    /** 买额度包额外多给的比例（0.1 = 多送 10%）。 */
    val packBonusRate: String = "0",
    /** 为 0 时沿用应用设置。 */
    val rpm: Int = 0,
    val maxConcurrent: Int = 0,
) {
    val isMember: Boolean get() = source != SOURCE_NONE && source.isNotEmpty()

    companion object {
        const val SOURCE_TIER = "tier"
        const val SOURCE_VIP = "vip"
        const val SOURCE_NONE = "none"
    }
}

/** 一次计价的倍率分解。[total] 是实际乘到单价上的数。 */
@Serializable
data class AegisAIFactorBreakdown(
    val base: String = "1",
    val member: String = "1",
    val level: String = "1",
    val time: String = "1",
    val total: String = "1",
    /** `multiply` 相乘、`best` 会员与闲时折扣只取更优惠的一个。 */
    val stacking: String = STACK_MULTIPLY,
    /** 此刻命中的时段，没有为 null。 */
    val window: AegisAITimeMatch? = null,
) {
    companion object {
        const val STACK_MULTIPLY = "multiply"
        const val STACK_BEST = "best"
    }
}

/** 命中的价格时段。 */
@Serializable
data class AegisAITimeMatch(
    val id: String = "",
    val name: String = "",
    /** 见 [AegisAITimeWindow.TONE_OFF_PEAK] 等。 */
    val tone: String = AegisAITimeWindow.TONE_NORMAL,
    val multiplier: String = "1",
    /** 这个时段结束、价格变化的时刻；一周内不变时为 null。 */
    val endsAt: String? = null,
)

/** 7 天内到期的额度包。 */
@Serializable
data class AegisAIExpiring(
    val amount: String = "0",
    val at: String = "",
)

/** 新人礼对这位用户的状态。 */
@Serializable
data class AegisAIWelcomeState(
    val enabled: Boolean = false,
    val amount: String = "0",
    /** 领到的批次有效天数，0 = 永久。 */
    val validityDays: Int = 0,
    val claimable: Boolean = false,
    val claimedAt: String? = null,
)

/** 额度商店对用户是否开放。 */
@Serializable
data class AegisAIStoreState(val enabled: Boolean = false)

/** 一组单价。token 类按每百万 token，[charPrice] 每百万字符，[minutePrice] 每分钟音频。 */
@Serializable
data class AegisAIPrice(
    val inputPrice: String = "0",
    val outputPrice: String = "0",
    val requestPrice: String = "0",
    val imagePrice: String = "0",
    val imagePrices: Map<String, String> = emptyMap(),
    val charPrice: String = "0",
    val minutePrice: String = "0",
    val free: Boolean = false,
)

/** `GET /ai/pricing`：价格方案。 */
@Serializable
data class AegisAIPricing(
    /** 时段按这个时区判断（IANA 名）。 */
    val timezone: String = "",
    /** 服务端此刻的时间。 */
    val now: String = "",
    /** 时段价格是否启用。 */
    val enabled: Boolean = false,
    val stacking: String = AegisAIFactorBreakdown.STACK_MULTIPLY,
    /** 全局倍率。 */
    val base: String = "1",
    val levels: List<AegisAILevelMultiplier> = emptyList(),
    /** 每日免费额度。 */
    val freeDaily: String = "0",
    /** 启用的时段。 */
    val windows: List<AegisAITimeWindow> = emptyList(),
    /** 我此刻对全部模型的倍率分解。 */
    val current: AegisAIFactorBreakdown = AegisAIFactorBreakdown(),
    val nextChangeAt: String? = null,
    /** 从今天本地零点起 7 天、对全部模型生效的时段，相邻同价段已合并。 */
    val timeline: List<AegisAITimelineSegment> = emptyList(),
    /** 我此刻生效的会员优惠。 */
    val member: AegisAIMemberBenefit = AegisAIMemberBenefit(),
    /** 每个已配置档位的优惠（按优先级），末尾是通用会员（source = vip，有任一优惠时才给）。 */
    val tiers: List<AegisAIMemberBenefit> = emptyList(),
)

/** 等级倍率：等级达到 [level] 时乘 [multiplier]。 */
@Serializable
data class AegisAILevelMultiplier(val level: Int = 0, val multiplier: String = "1")

/** 一个价格时段（与控制台设置同一结构）。 */
@Serializable
data class AegisAITimeWindow(
    val id: String = "",
    val name: String = "",
    val tone: String = TONE_NORMAL,
    /** 1 = 周一 … 7 = 周日，空表示每天；跨零点的时段按开始那一天判星期。 */
    val weekdays: List<Int> = emptyList(),
    /** `HH:MM`，半开区间；end ≤ start 表示跨零点，相等表示全天。 */
    val start: String = "",
    val end: String = "",
    val multiplier: String = "1",
    /** 空表示全部模型 / 全部种类。 */
    val models: List<String> = emptyList(),
    val kinds: List<String> = emptyList(),
    /** 活动日期 `YYYY-MM-DD`（含当天），可空。 */
    val from: String? = null,
    val to: String? = null,
    val enabled: Boolean = true,
) {
    companion object {
        const val TONE_OFF_PEAK = "off_peak"
        const val TONE_PEAK = "peak"
        const val TONE_PROMO = "promo"
        const val TONE_NORMAL = "normal"
    }
}

/** 时间轴上价格不变的一段。[windowId] 为空是标准价。 */
@Serializable
data class AegisAITimelineSegment(
    val start: String = "",
    val end: String = "",
    val windowId: String = "",
    val name: String = "",
    val tone: String = AegisAITimeWindow.TONE_NORMAL,
    val multiplier: String = "1",
)

/** `GET /ai/packages`：额度商店。 */
@Serializable
data class AegisAIPackageList(
    val enabled: Boolean = false,
    /** 购买须知（纯文本）。 */
    val notice: String = "",
    val member: AegisAIMemberBenefit = AegisAIMemberBenefit(),
    /** 钱包余额（元）。 */
    val wallet: String = "0",
    val items: List<AegisAIPackage> = emptyList(),
)

/** 用户看到的一个额度包：包本身、我的报价、能否购买。 */
@Serializable
data class AegisAIPackage(
    val id: Long = 0,
    val name: String = "",
    val subtitle: String = "",
    val description: String = "",
    /** 角标文字。 */
    val badge: String = "",
    /** 配色：`blue` `violet` `amber` `emerald` `rose` `slate`。 */
    val tone: String = "",
    /** 主推。 */
    val featured: Boolean = false,
    /** 额度 / 包自带赠送 / 会员多送 / 合计到账。 */
    val points: String = "0",
    val bonusPoints: String = "0",
    val memberBonusPoints: String = "0",
    val totalPoints: String = "0",
    /** 售价 / 划线价（"0" 不显示）/ 我的实付 / 会员省下的钱（元）。 */
    val price: String = "0",
    val originalPrice: String = "0",
    val finalPrice: String = "0",
    val memberSaving: String = "0",
    /** 会员优惠的说法，如「SVIP 85 折」「多送 10%」。 */
    val discountLabel: String = "",
    /** 每万额度多少元（展示用）。 */
    val unitPrice: String = "0",
    /** 有效天数，0 = 永久。 */
    val validityDays: Int = 0,
    /** `all` / `vip`。 */
    val audience: String = "all",
    val tierIds: List<Long> = emptyList(),
    /** 是否享受会员的额度包折扣与多送。 */
    val memberDiscount: Boolean = false,
    /** 每人累计 / 每人每月限购，0 不限。 */
    val perUserLimit: Int = 0,
    val monthlyLimit: Int = 0,
    val purchased: Int = 0,
    val purchasedThisMonth: Int = 0,
    /** 总库存，0 不限。 */
    val stock: Int = 0,
    val sold: Int = 0,
    /** 剩余库存，不限时为 null。 */
    val remainingStock: Int? = null,
    val startsAt: String? = null,
    val endsAt: String? = null,
    val purchasable: Boolean = false,
    /** 不可购买的原因，见 [REASON_STORE_CLOSED] 等。 */
    val reason: String = "",
    /** 余额 ≥ 实付（不影响 [purchasable]）。 */
    val affordable: Boolean = false,
) {
    companion object {
        const val REASON_STORE_CLOSED = "store_closed"
        const val REASON_MEMBERS_ONLY = "members_only"
        const val REASON_TIER_REQUIRED = "tier_required"
        const val REASON_NOT_STARTED = "not_started"
        const val REASON_ENDED = "ended"
        const val REASON_SOLD_OUT = "sold_out"
        const val REASON_LIMIT_REACHED = "limit_reached"
        const val REASON_MONTHLY_LIMIT_REACHED = "monthly_limit_reached"
    }
}

/** 一笔额度包订单。 */
@Serializable
data class AegisAIPackageOrder(
    val orderNo: String = "",
    val packageId: Long = 0,
    val packageName: String = "",
    /** 标价 / 实付 / 优惠（元）。 */
    val listPrice: String = "0",
    val price: String = "0",
    val discount: String = "0",
    val discountLabel: String = "",
    /** 额度 / 赠送（包自带 + 会员多送）/ 合计。 */
    val points: String = "0",
    val bonusPoints: String = "0",
    val totalPoints: String = "0",
    val validityDays: Int = 0,
    val expiresAt: String? = null,
    val lotId: Long = 0,
    val walletTxnNo: String = "",
    /** `paid` / `refunded`。 */
    val status: String = "",
    val refundAmount: String = "0",
    val refundPoints: String = "0",
    val refundReason: String = "",
    val refundedAt: String? = null,
    val createdAt: String = "",
) {
    companion object {
        const val STATUS_PAID = "paid"
        const val STATUS_REFUNDED = "refunded"
    }
}

/** 一个额度批次。 */
@Serializable
data class AegisAICreditLot(
    val id: Long = 0,
    /** `purchase` 购买、`welcome` 新人礼、`admin` 管理员发放、`grant` 发放规则 / 活动。 */
    val origin: String = "",
    val title: String = "",
    /** 总量 / 可用 / 调用暂扣中 / 已用。 */
    val amount: String = "0",
    val remaining: String = "0",
    val frozen: String = "0",
    val used: String = "0",
    val orderNo: String = "",
    val packageId: Long = 0,
    /** 到期时间，null 永久。 */
    val expiresAt: String? = null,
    /** `active` / `exhausted` / `expired` / `revoked`。 */
    val status: String = "",
    val createdAt: String = "",
) {
    companion object {
        const val ORIGIN_PURCHASE = "purchase"
        const val ORIGIN_WELCOME = "welcome"
        const val ORIGIN_ADMIN = "admin"
        const val ORIGIN_GRANT = "grant"
        const val STATUS_ACTIVE = "active"
        const val STATUS_EXHAUSTED = "exhausted"
        const val STATUS_EXPIRED = "expired"
        const val STATUS_REVOKED = "revoked"
        const val SCOPE_ACTIVE = "active"
        const val SCOPE_ALL = "all"
    }
}

/** `POST /ai/packages/{id}/purchase` 的结论。[replayed] 为 true 表示同一 requestId 重放、返回的是原订单。 */
@Serializable
data class AegisAIPurchaseResult(
    val order: AegisAIPackageOrder = AegisAIPackageOrder(),
    val lot: AegisAICreditLot = AegisAICreditLot(),
    /** 扣款后的钱包余额（元）。 */
    val wallet: String = "0",
    /** 到账后的 AI 额度合计。 */
    val balance: String = "0",
    val replayed: Boolean = false,
)

/** `POST /ai/welcome` 的结论。 */
@Serializable
data class AegisAIWelcomeResult(
    val lot: AegisAICreditLot = AegisAICreditLot(),
    val balance: String = "0",
)

/** 一条额度流水（永久额度与额度包）。 */
@Serializable
data class AegisAICreditEntry(
    val id: Long = 0,
    /** 见 [KIND_GRANT] 等。 */
    val kind: String = "",
    /** `credit` 永久额度 / `pack` 额度包。 */
    val bucket: String = "",
    /** 涉及的批次（`pack` 时）。 */
    val lotId: Long = 0,
    /** 变动（正负）。 */
    val amount: String = "0",
    /** 这一桶变动后的余额。 */
    val balanceAfter: String = "0",
    /** 变动后的 AI 额度合计（永久 + 额度包，不含暂扣中）。 */
    val totalAfter: String = "0",
    val refType: String = "",
    val refId: String = "",
    val remark: String = "",
    val createdAt: String = "",
) {
    companion object {
        const val KIND_GRANT = "grant"
        const val KIND_CONSUME = "consume"
        const val KIND_REFUND = "refund"
        const val KIND_ADJUST = "adjust"
        const val KIND_PURCHASE = "purchase"
        const val KIND_WELCOME = "welcome"
        const val KIND_EXPIRE = "expire"
        const val KIND_REVOKE = "revoke"
        const val KIND_ORDER_REFUND = "order_refund"
        const val BUCKET_CREDIT = "credit"
        const val BUCKET_PACK = "pack"
    }
}

/** 网关的分页结果。 */
@Serializable
data class AegisAIPage<T>(
    val items: List<T> = emptyList(),
    val total: Long = 0,
    val page: Int = 1,
    val limit: Int = 20,
) {
    /** 后面还有没有。 */
    val hasMore: Boolean get() = page.toLong() * limit < total
}

/** `GET /ai/models` 的一项：可调用的模型与有效单价（已乘上本人此刻的倍率）。 */
@Serializable
data class AegisAIModelInfo(
    val model: String = "",
    /** `chat` / `image` / `embedding` / … */
    val kind: String = "",
    val displayName: String = "",
    val description: String = "",
    val vipOnly: Boolean = false,
    /** 会员专享而本人不是会员时为 false。 */
    val available: Boolean = true,
    val free: Boolean = false,
    val inputPrice: String = "0",
    val outputPrice: String = "0",
    val requestPrice: String = "0",
    val imagePrice: String = "0",
    val imagePrices: Map<String, String> = emptyMap(),
    val charPrice: String = "0",
    val minutePrice: String = "0",
    /** 不含时段倍率的单价（已乘会员与等级倍率）；旧版服务端没有时为 null。 */
    val basePrice: AegisAIPrice? = null,
    /** 此刻对这个模型的合计倍率。 */
    val factor: String = "1",
    /** 此刻对这个模型生效的时段，没有为 null。 */
    val window: AegisAITimeMatch? = null,
    /** 后台给这个模型设的输出上限，0 表示没设。 */
    val maxOutputTokens: Int = 0,
    /** 对话模型的能力声明。 */
    val capabilities: AegisAICapabilities? = null,
)

/** 对话模型的能力（后台在模型上配置）。 */
@Serializable
data class AegisAICapabilities(
    val vision: Boolean = false,
    val tools: Boolean = true,
    val reasoning: Boolean = false,
    val reasoningEfforts: List<String> = emptyList(),
    val contextLength: Long = 0,
    val maxOutput: Long = 0,
)

@Serializable
internal data class AegisAIModelList(val items: List<AegisAIModelInfo> = emptyList())
