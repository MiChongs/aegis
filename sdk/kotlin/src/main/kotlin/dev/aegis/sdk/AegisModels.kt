package dev.aegis.sdk

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

/**
 * `/config` 的响应模型。
 *
 * 客户端**只需要硬编码 baseUrl 与 appKey**，其余一切（怎么包装、走哪些路径、
 * 有哪些登录方式、错误码怎么分类）都从这里读。管理员在控制台改了配置，
 * 客户端下次拉 config 就跟上，不必发版。
 */
@Serializable
data class AegisConfig(
    val protocolVersion: String = "",
    val app: AppBrief = AppBrief(),
    val auth: AuthCapability = AuthCapability(),
    val security: SecuritySpec = SecuritySpec(),
    val serverTime: ServerTime = ServerTime(),
    val limits: Limits = Limits(),
    val endpoints: Map<String, String> = emptyMap(),
    val operations: List<Operation> = emptyList(),
    val errors: List<ErrorDescriptor> = emptyList(),
    /** 版本管控通道（公钥与参数）；服务端未提供时为 null。见 [AegisClientControlApi]。 */
    val clientControl: AegisClientControlSpec? = null,
)

@Serializable
data class AppBrief(val key: String = "", val name: String = "", val status: Boolean = false)

@Serializable
data class AuthCapability(
    val identifiers: List<String> = emptyList(),
    val loginMethods: List<String> = emptyList(),
    val registerMethods: List<String> = emptyList(),
    val registrationSchema: List<RegistrationField> = emptyList(),
    val captcha: CaptchaRequirement = CaptchaRequirement(),
    @SerialName("autoLoginAfterRegister") val autoLogin: Boolean = false,
    val registerEnabled: Boolean = false,
    val loginEnabled: Boolean = false,
    val oauthProviders: List<JsonElement> = emptyList(),
    /** 注册表单上的邀请码字段：出不出现、是否必填都由它决定，见 [InviteCapability]。 */
    val invite: InviteCapability = InviteCapability(),
    /** 通行密钥（Passkey）能力，见 [PasskeyCapability]。 */
    val passkey: PasskeyCapability = PasskeyCapability(),
)

/**
 * 通行密钥（Passkey）能力。
 *
 * [rpId] 只在应用配了独立依赖方（控制台「Passkey」区块）时下发。原生客户端没有它就不该画入口：
 * 系统凭据管理器要求 RP ID 是一个发布了 assetlinks / apple-app-site-association 的域名，
 * 平台级配置给不出这个值，仪式会在 origin 校验那一步失败。
 */
@Serializable
data class PasskeyCapability(
    val enabled: Boolean = false,
    val rpId: String = "",
)

/**
 * 注册表单上的邀请码字段。
 *
 * [mode] 为 `hidden` 时不画输入框（注册后仍可能在 [bindWindowHours] 小时内到邀请页补填），
 * `optional` 为选填，`required` 为必填 —— 没有邀请码不能注册。填的码随
 * [AegisAuthApi.register] 的 `inviteCode` 提交。
 */
@Serializable
data class InviteCapability(
    val enabled: Boolean = false,
    /** `hidden` / `optional` / `required`（JSON 字段名为 `field`）。 */
    @SerialName("field") val mode: String = "hidden",
    val label: String = "邀请码",
    val placeholder: String = "",
    val maxLength: Int = 32,
    val bindWindowHours: Int = 0,
) {
    val shown: Boolean get() = enabled && mode != "hidden"
    val required: Boolean get() = enabled && mode == "required"
}

/**
 * 各入口到底要不要图形验证码 —— 服务端算好的**结论**，客户端照做即可。
 *
 * 这件事在服务端由三处独立开关共同决定（接入协议策略、应用验证码配置的分场景
 * 开关、平台级的短信前置验证码），分属三个管理入口。客户端不该去理解它们的关系，
 * 更不该把判断复制一遍 —— 复制就意味着管理员改了配置而客户端不跟着改。
 *
 * 不带验证码调一个要求验证码的接口会得到 40015，而用户在界面上看不到任何线索：
 * 表单里根本没有验证码输入框。所以登录 / 注册页要按这里的结论决定是否先调
 * `/captcha` 并渲染那一栏。
 */
@Serializable
data class CaptchaRequirement(
    /** 调 `/auth/login` 前是否必须先取验证码。 */
    val login: Boolean = false,
    /** 调 `/auth/register` 前是否必须先取验证码。 */
    val register: Boolean = false,
    /**
     * 调 `/auth/sms/code` 前是否必须先取图形验证码（防轰炸）。
     *
     * 由平台级配置决定，不区分登录 / 注册 —— 服务端就是一个开关，
     * 这里不造两个恒等的字段让人以为它们会不同。
     */
    val sms: Boolean = false,
    /** 调 `/auth/password/recovery/start` 前是否必须先取验证码（purpose: `reset_password`）。 */
    val recovery: Boolean = false,
)

@Serializable
data class RegistrationField(
    val name: String = "",
    val type: String = "text",
    val required: Boolean = false,
    val mutable: Boolean = false,
    val label: String = "",
    val placeholder: String = "",
)

@Serializable
data class SecuritySpec(
    /** standard / signed / sealed */
    val level: String = AegisSecurityLevel.STANDARD,
    val appKeyHeader: String = "X-Aegis-App-Key",
    val signature: SignatureSpec? = null,
    val transport: TransportSpec? = null,
)

@Serializable
data class SignatureSpec(
    val scheme: String = "",
    val header: String = "X-Aegis-Signature",
    val timestampHeader: String = "X-Aegis-Timestamp",
    val nonceHeader: String = "X-Aegis-Nonce",
    /** 当前应当使用的签名版本前缀。v1 不覆盖 query string，只对无 query 的请求有效。 */
    val version: String = "v2=",
    val canonical: String = "",
    val canonicalLegacy: String = "",
    @SerialName("maxClockSkewSeconds") val maxClockSkew: Int = 300,
)

@Serializable
data class TransportSpec(
    val protocol: String = "",
    val algorithms: List<String> = emptyList(),
    val activeKeyId: String = "",
    val publicKeys: List<PublicTransportKey> = emptyList(),
    @SerialName("maxClockSkewSeconds") val maxClockSkew: Int = 300,
    @SerialName("replayWindowSeconds") val replayWindow: Int = 300,
    val hkdfSalt: String = "",
    val payloadParam: String = "_payload",
    val bodylessMethods: List<String> = listOf("GET", "DELETE", "HEAD"),
    val plainContentTypeHeader: String = "X-Aegis-Plain-Content-Type",
) {
    /**
     * 按 activeKeyId 取公钥。**不要缓存单个公钥** —— 服务端会轮换，
     * 旧密钥最多保留 24 小时，之后请求一律 40074。
     */
    fun activePublicKey(): PublicTransportKey? = publicKeys.firstOrNull { it.keyId == activeKeyId }
}

@Serializable
data class PublicTransportKey(
    val keyId: String = "",
    val algorithm: String = "",
    val publicKey: String = "",
    val status: String = "",
)

@Serializable
data class ServerTime(val unix: Long = 0, val iso: String = "")

@Serializable
data class Limits(
    val maxRequestBytes: Long = 8L * 1024 * 1024,
    val maxUploadBytes: Long = 32L * 1024 * 1024,
    val clockSkewSeconds: Int = 300,
    val nonceMinLength: Int = 8,
    val nonceMaxLength: Int = 128,
)

/** 接口目录条目。生成式客户端与调试工具用它构造请求，手写客户端可以忽略。 */
@Serializable
data class Operation(
    val key: String = "",
    val method: String = "GET",
    val path: String = "",
    val auth: Boolean = false,
    val unwrapped: Boolean = false,
    val upload: Boolean = false,
    val summary: String = "",
)

/** 错误码目录。[recovery] 告诉客户端这个错能不能自动重试、重试前要做什么。 */
@Serializable
data class ErrorDescriptor(
    val code: Int = 0,
    val name: String = "",
    val message: String = "",
    val recovery: String = AegisRecovery.NONE,
    val hint: String = "",
)

object AegisSecurityLevel {
    const val STANDARD = "standard"
    const val SIGNED = "signed"
    const val SEALED = "sealed"
}

/** 与服务端 authprotocol.Recovery* 一一对应。 */
object AegisRecovery {
    const val NONE = "none"
    const val REFRESH_CONFIG = "refresh-config"
    const val SYNC_CLOCK = "sync-clock"
    const val NEW_NONCE = "new-nonce"
    const val REFRESH_TOKEN = "refresh-token"
    const val REAUTH = "reauth"
}

/** 平台统一响应信封：`code == 200` 才是成功。 */
@Serializable
data class AegisEnvelope(
    val code: Int = 0,
    val message: String = "",
    val data: JsonElement? = null,
)

/**
 * 登录 / 注册 / 刷新的结果。
 *
 * [requiresSecondFactor] 为 true 时 [accessToken] 是空的，会话还没建立 ——
 * 拿 [challenge] 里的 `challengeId` 去调 `/auth/2fa/verify` 才拿得到令牌。
 * 把这一步当成"登录成功"是接入时最常见的错。
 */
@Serializable
data class AegisSession(
    val accessToken: String = "",
    val refreshToken: String = "",
    val expiresAt: String = "",
    val refreshExpiresAt: String = "",
    val tokenType: String = "",
    val userId: Long = 0,
    /** 号码（5–12 位、可以更换，docs/user-number.md）：展示用；本地数据的键仍是 [userId]。 */
    val number: Long = 0,
    val account: String = "",
    val provider: String = "",
    val requiresSecondFactor: Boolean = false,
    val authenticationState: String = "",
    val challenge: SecondFactorChallenge? = null,
    /**
     * 账号被标记为必须改密（例如批量导入后设了统一密码）。
     * 为 true 时应当立刻引导用户改密，而不是放进主界面。
     */
    val passwordChangeRequired: Boolean = false,
    /**
     * 注册时填写的邀请码的结论（只有注册接口会带）：`bound`、`message`、`rewards`、`inviter`。
     * 注册不因邀请码失败而失败，`bound=false` 时 `message` 说明原因。
     */
    val invite: JsonElement? = null,
    /**
     * 原生第三方登录（[AegisAuthApi.oauthExchange] 带 `deferRegistration`）核实的身份还没关联账号。
     * 此时没有令牌，让用户选「快速注册」或「绑定已有账号」。
     */
    val oauthLink: OAuthLinkTicket? = null,
    /**
     * 账号处于注销冷静期或待审核时出现：`status`、`mode`、`scheduledAt`；
     * `cancelled` 为 true 表示这次登录撤销了冷静期（应用配置为登录即撤销）。
     */
    val accountDeletion: AccountDeletionLoginNotice? = null,
    /**
     * 卡密登录（`method = "cardkey"`）才有：这张授权卡的到期时间（永久为空）、可绑设备数、已绑设备数，
     * 以及这次是不是首次激活（首次才发随卡权益）。
     */
    val authorization: CardKeyAuthorization? = null,
    /**
     * 登录设备数已满、应用设置为让用户选择时出现（`authenticationState = device_limit_required`）。
     * 此时没有令牌：把 [DeviceLimitChallenge.devices] 列给用户勾选，至少 [DeviceLimitChallenge.mustRemove] 台，
     * 再调 [AegisAuthApi.resolveDeviceLimit] 完成登录。只有在 Builder 里声明了
     * [AegisClient.Builder.deviceLimitChoice] 才会收到它，否则服务端按应用的兜底方式处理。
     */
    val deviceLimit: DeviceLimitChallenge? = null,
) {
    /** 拿到可用令牌才算登录完成。 */
    val isAuthenticated: Boolean get() = accessToken.isNotEmpty()
}

/** 登录设备数已满时让用户选择下线哪台设备，见 [AegisSession.deviceLimit]。 */
@Serializable
data class DeviceLimitChallenge(
    /** 选择页凭证，5 分钟有效 */
    val ticket: String = "",
    val expiresAt: String = "",
    /** 触发的维度：`total` 总数 / `class` 某一终端类别 */
    val dimension: String = "",
    val requirements: List<DeviceLimitRequirement> = emptyList(),
    /** 至少勾选几台 */
    val mustRemove: Int = 1,
    val message: String = "",
    val devices: List<LoginDevice> = emptyList(),
)

/** 某个维度超了多少。 */
@Serializable
data class DeviceLimitRequirement(
    /** `total` / `class` / `new_device` */
    val dimension: String = "",
    /** 维度为 `class` 时：`mobile` / `desktop` / `web` / `other` */
    @SerialName("class") val deviceClass: String = "",
    val limit: Int = 0,
    val current: Int = 0,
    val mustRemove: Int = 0,
)

/**
 * 一台已登录的设备：同一台设备上的多次登录合成一台。
 * [key] 用来下线它；[deviceInfo] 是服务端按设备字典给出的展示信息（名称、厂商图标、设备图等）。
 */
@Serializable
data class LoginDevice(
    val key: String = "",
    val deviceId: String = "",
    val platform: String = "",
    /** 终端类别：`mobile` / `desktop` / `web` / `other` */
    @SerialName("class") val deviceClass: String = "",
    val name: String = "",
    val deviceInfo: JsonElement? = null,
    val ip: String = "",
    val provider: String = "",
    val sessions: Int = 0,
    val loginAt: String = "",
    val lastActiveAt: String = "",
    /** 是不是本机 */
    val current: Boolean = false,
    /** 只在选择页有意义：勾它能不能解决超限（例如只超了手机数时，电脑勾了也没用） */
    val eligible: Boolean = true,
) {
    /** [deviceInfo] 里的展示名，没有时退回登录时保存的名称。 */
    val displayName: String
        get() = (deviceInfo as? kotlinx.serialization.json.JsonObject)
            ?.get("name")?.let { (it as? kotlinx.serialization.json.JsonPrimitive)?.content }
            ?.takeIf { it.isNotBlank() } ?: name
}

/** 我的登录设备与生效上限，见 [AegisMeApi.devices]。 */
@Serializable
data class LoginDeviceStatus(
    /** 应用是否开启了设备数控制 */
    val enabled: Boolean = false,
    val limits: DeviceLimits = DeviceLimits(),
    val usage: DeviceUsage = DeviceUsage(),
    val devices: List<LoginDevice> = emptyList(),
    /** 同一台设备再次登录时：`replace` 顶掉旧登录 / `coexist` 保留 */
    val sameDevice: String = "",
    /** 超限时：`kick_oldest` / `kick_idle` / `reject` / `ask` */
    val overflow: String = "",
    val newDevicesInWindow: Int = 0,
)

/** 此刻生效的上限。[maxDevices] 为 0 表示不限总数；[classLimits] 里没有的类别不单独限制。 */
@Serializable
data class DeviceLimits(
    val unlimited: Boolean = true,
    val maxDevices: Int = 0,
    val classLimits: Map<String, Int> = emptyMap(),
    /** `disabled` / `default` / `tier` / `override` / `exempt` */
    val source: String = "",
    val sourceLabel: String = "",
)

/** 已登录设备的占用。 */
@Serializable
data class DeviceUsage(
    val total: Int = 0,
    val byClass: Map<String, Int> = emptyMap(),
)

/** 授权卡登录时随会话下发的授权快照。 */
@Serializable
data class CardKeyAuthorization(
    val code: String = "",
    val expiresAt: String? = null,
    val maxDevices: Int = 0,
    val deviceCount: Int = 0,
    val firstActivation: Boolean = false,
)

/**
 * 待关联的第三方身份。[ticket] 一次性、10 分钟有效；[nickname] / [avatar] 只用于展示。
 * [allowRegister] / [allowBind] 是此刻两条路能不能走的结论，据此决定显示哪个按钮。
 */
@Serializable
data class OAuthLinkTicket(
    val ticket: String = "",
    val provider: String = "",
    val displayName: String = "",
    val nickname: String = "",
    val avatar: String = "",
    val allowRegister: Boolean = false,
    val allowBind: Boolean = false,
    val expiresAt: String = "",
)

/** 登录结果里的注销状态，见 [AegisSession.accountDeletion]。 */
@Serializable
data class AccountDeletionLoginNotice(
    val status: String = "",
    val mode: String = "",
    val scheduledAt: String? = null,
    val cancelled: Boolean = false,
)

@Serializable
data class SecondFactorChallenge(
    val challengeId: String = "",
    val state: String = "",
    val methods: List<String> = emptyList(),
    val expiresAt: String = "",
)

/**
 * 激励广告场景的一项奖励（`/ads/rewarded` 的 `scenes[].rewards[]`）。
 *
 * [amount] 的含义随 [type] 变：会员天数是天、积分是分、功能时长是分钟；
 * [refId] 只有会员套餐用（套餐 ID），[feature] 只有功能时长用（功能标识）。
 */
@Serializable
data class AegisRewardedAdReward(
    val type: String = "",
    val amount: Long = 0L,
    val refId: Long = 0L,
    val feature: String = "",
) {
    companion object {
        const val VIP_PLAN = "vip_plan"
        const val VIP_DAYS = "vip_days"
        const val INTEGRAL = "integral"
        const val EXPERIENCE = "experience"
        const val LOTTERY_DRAWS = "lottery_draws"

        /** 功能时长：给 [feature] 加 [amount] 分钟，用 [AegisEngagementApi.leaseFeatureTime] 按实际使用扣。 */
        const val FEATURE_MINUTES = "feature_minutes"
    }
}

/** `GET /features/time/{feature}`：一个功能上的时长。秒数都是整秒。 */
@Serializable
data class AegisFeatureTime(
    val feature: String = "",
    /** 功能目录里的展示名，目录里没有时为空串。 */
    val name: String = "",
    val balanceSeconds: Long = 0L,
    /** 免扣：当前是会员，或单独拥有这个功能。免扣时租约不扣余额。 */
    val exempt: Boolean = false,
    val earnedSeconds: Long = 0L,
    /** 实际用掉的：租出减去退回。 */
    val usedSeconds: Long = 0L,
)

/** `POST /features/time/{feature}/leases`：一次租约。 */
@Serializable
data class AegisFeatureTimeLease(
    /** 免扣时为 true：没有扣余额，[leaseNo] 与 [expiresAt] 为空，直接使用即可。 */
    val exempt: Boolean = false,
    val leaseNo: String = "",
    /** 实际租到的秒数（余额不足时少于所求）。 */
    val seconds: Int = 0,
    /** 租出后的余额。 */
    val balanceSeconds: Long = 0L,
    /** 租约到期时间（RFC3339），到期后再结算不退款；免扣时为空串。 */
    val expiresAt: String = "",
)

/** `POST /features/time/leases/{leaseNo}/settle`：一次结算。 */
@Serializable
data class AegisFeatureTimeSettlement(
    val leaseNo: String = "",
    val feature: String = "",
    /** `settled` 已结算并退回没用完的部分；`expired` 过期后才结算，不退款。 */
    val status: String = "",
    val seconds: Int = 0,
    val usedSeconds: Int = 0,
    val refundedSeconds: Int = 0,
    /** 结算后的当前余额。 */
    val balanceSeconds: Long = 0L,
) {
    companion object {
        const val STATUS_SETTLED = "settled"
        const val STATUS_EXPIRED = "expired"
    }
}

/** 功能时长的业务错误码，对应 /config 的 errors 目录。 */
object AegisFeatureTimeCodes {
    /** 余额为 0，租不出来：引导用户看激励广告领时长，或开通会员。 */
    const val FEATURE_TIME_EXHAUSTED: Int = 40328

    /** 租约不存在或不属于当前用户。 */
    const val FEATURE_TIME_LEASE_NOT_FOUND: Int = 40406
}

/** `GET /me/account-setup`：快速登录建的号还缺什么、现在该怎么提醒。 */
@Serializable
data class AegisAccountSetupStatus(
    /** 还有没完成的项（按应用策略要求的算）。 */
    val required: Boolean = false,
    /** 用户名还是系统生成的，需要设置。 */
    val accountRequired: Boolean = false,
    /** 还没有密码，需要设置。 */
    val passwordRequired: Boolean = false,
    /** none / dialog / banner。dialog 只出现一次，展示后调 markAccountSetupPrompted。 */
    val prompt: String = PROMPT_NONE,
    /** 横幅能不能关闭。 */
    val dismissible: Boolean = true,
    val account: String = "",
    /** 建号方式：oauth / sms / cardkey；自己注册的账号为空串。 */
    val source: String = "",
    /** 第三方渠道（source = oauth 时），如 qq。 */
    val provider: String = "",
    val snoozedUntil: String? = null,
    val completedAt: String? = null,
    /** 可直接选用的用户名（服务端已确认未被占用）。 */
    val suggestions: List<String> = emptyList(),
    val rules: AegisUsernameRules = AegisUsernameRules(),
) {
    companion object {
        const val PROMPT_NONE = "none"
        const val PROMPT_DIALOG = "dialog"
        const val PROMPT_BANNER = "banner"
    }
}

/** 用户名规则：长度按字符计，[hint] 可直接显示在输入框下方。 */
@Serializable
data class AegisUsernameRules(
    val minLength: Int = 4,
    val maxLength: Int = 24,
    val hint: String = "",
)

/** `POST /me/account-setup` 的结果。 */
@Serializable
data class AegisAccountSetupResult(
    val status: AegisAccountSetupStatus = AegisAccountSetupStatus(),
    /** 现在的账号名。 */
    val account: String = "",
    val accountChanged: Boolean = false,
    val passwordSet: Boolean = false,
)

/** `POST /me/account-setup/check`：[message] 是不可用的原因。 */
@Serializable
data class AegisUsernameCheck(
    val available: Boolean = false,
    val message: String = "",
)
