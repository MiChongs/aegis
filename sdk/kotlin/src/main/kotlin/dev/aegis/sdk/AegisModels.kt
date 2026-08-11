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
)

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
) {
    /** 拿到可用令牌才算登录完成。 */
    val isAuthenticated: Boolean get() = accessToken.isNotEmpty()
}

@Serializable
data class SecondFactorChallenge(
    val challengeId: String = "",
    val state: String = "",
    val methods: List<String> = emptyList(),
    val expiresAt: String = "",
)
