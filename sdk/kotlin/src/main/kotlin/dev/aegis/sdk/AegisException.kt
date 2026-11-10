package dev.aegis.sdk

import java.io.IOException
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull

/**
 * 所有失败都收敛到这一个异常类型上，按 [kind] 分类。
 *
 * 分类不是按 HTTP 状态码，而是按**调用方该怎么办**：
 *
 *   TRANSPORT —— 包装方式不对（签名、加密、时钟、nonce）。业务代码无能为力，
 *                这是接入期的问题，运行期几乎只可能是时钟漂了或密钥轮换了。
 *   AUTH      —— 令牌过期或不属于本应用。该刷新令牌或回登录页。
 *   BUSINESS  —— 服务端按业务规则拒绝（密码错、验证码错、未启用某方式）。
 *                这一类要原样把 message 显示给用户。
 *   NETWORK   —— 连不上、超时。可以重试。
 *
 * 把 TRANSPORT 和 BUSINESS 混在一起是接入期最费时间的一件事：
 * 「签名算错了」和「密码输错了」都返回 400，不分类的话每次都要人肉看 code。
 */
class AegisException private constructor(
    val kind: Kind,
    /** 平台业务码，见 /config 的 errors 目录；网络错误为 0。 */
    val code: Int,
    message: String,
    /** 该错误的建议恢复动作，取自 /config 的 errors 目录。 */
    val recovery: String = AegisRecovery.NONE,
    /** 服务端给出的修复提示，接入期直接照做即可。 */
    val hint: String = "",
    cause: Throwable? = null,
    /** NETWORK 类的细分，见 [AegisNetworkReason]；其他类为空串。 */
    val networkReason: String = AegisNetworkReason.NONE,
    /** 收到了 HTTP 响应但不是平台信封时的状态码（如 CDN 的 502）；其余为 0。 */
    val httpStatus: Int = 0,
    /**
     * 业务失败时信封里的 `data`（多数错误没有，为 null）。少数错误在这里带结构化详情，
     * 例如 40301 / 40302 的 `data.suspension`（封禁详情与申诉凭证）、42921 的 `data.nextAllowedAt`。
     */
    val data: JsonElement? = null,
) : IOException(message, cause) {

    enum class Kind { TRANSPORT, AUTH, BUSINESS, NETWORK }

    /** 拿 refreshToken 换一次新令牌就能继续。 */
    val isRetryableWithRefresh: Boolean get() = recovery == AegisRecovery.REFRESH_TOKEN

    /** 重新拉 /config 后重试一次即可（典型场景：传输密钥刚轮换过）。 */
    val isRetryableAfterConfigRefresh: Boolean get() = recovery == AegisRecovery.REFRESH_CONFIG

    /**
     * 本机的登录已被服务端收回：账号在别的设备登录、超出登录设备数被挤下线（40130），
     * 或在设备管理里被移除（40131）。访问令牌与刷新令牌都已失效，SDK 已清掉本地令牌，
     * 应当把 [message] 告诉用户并回到登录页，不要再尝试刷新。
     */
    val isSignedOut: Boolean get() = code in SIGNED_OUT_CODES

    /** 调用方自己取消的（见 [AegisAICancellation]），不是失败。 */
    val isCanceled: Boolean get() = networkReason == AegisNetworkReason.CANCELED

    override fun toString(): String =
        "AegisException(kind=$kind, code=$code, message=$message" +
            (if (networkReason.isNotEmpty()) ", networkReason=$networkReason" else "") +
            (if (httpStatus != 0) ", httpStatus=$httpStatus" else "") +
            (if (hint.isNotEmpty()) ", hint=$hint" else "") + ")"

    companion object {
        /** 网关级错误码区间：这些错在请求进到业务逻辑之前就被拦下了。 */
        private val TRANSPORT_CODES = setOf(
            40070, 40071, 40072, 40073, 40074, 40075, 40076, 40077, 40078, 40079,
            40084, 40174, 40175, 40176, 40970, 42670, 50370, 50372,
        )
        private val AUTH_CODES = setOf(40100, 40110, 40372)

        /** 本机登录被收回，见 [isSignedOut]。 */
        internal val SIGNED_OUT_CODES = setOf(40130, 40131)

        @JvmStatic
        fun transport(message: String, cause: Throwable? = null): AegisException =
            AegisException(Kind.TRANSPORT, 0, message, cause = cause)

        @JvmStatic
        @JvmOverloads
        fun network(
            message: String,
            cause: Throwable? = null,
            reason: String = (cause as? IOException)?.networkReason() ?: AegisNetworkReason.IO,
            httpStatus: Int = 0,
        ): AegisException =
            AegisException(Kind.NETWORK, 0, message, cause = cause, networkReason = reason, httpStatus = httpStatus)

        /**
         * 按业务码归类。[descriptor] 来自 /config 的 errors 目录 ——
         * 有它就能给出「该怎么办」，没有也不影响归类。
         */
        @JvmStatic
        @JvmOverloads
        fun fromCode(
            code: Int,
            message: String,
            descriptor: ErrorDescriptor? = null,
            data: JsonElement? = null,
        ): AegisException {
            val kind = when (code) {
                in AUTH_CODES, in SIGNED_OUT_CODES -> Kind.AUTH
                in TRANSPORT_CODES -> Kind.TRANSPORT
                else -> Kind.BUSINESS
            }
            return AegisException(
                kind = kind,
                code = code,
                message = message.ifEmpty { descriptor?.message ?: "请求失败" },
                recovery = descriptor?.recovery
                    ?: if (code in SIGNED_OUT_CODES) AegisRecovery.REAUTH else defaultRecovery(kind),
                hint = descriptor?.hint.orEmpty(),
                data = data?.takeUnless { it is JsonNull },
            )
        }

        /**
         * AI 中转（`/api/ai/v1`）的错误：那一组接口回 OpenAI / Anthropic 形状的错误信封而不是平台信封。
         * [code] 是信封里的 `aegis_code`（上游自己的错误没有，为 0），[httpStatus] 是同步调用会回的状态码。
         */
        internal fun relay(code: Int, message: String, httpStatus: Int): AegisException {
            val kind = when (code) {
                in AUTH_CODES, in SIGNED_OUT_CODES -> Kind.AUTH
                else -> Kind.BUSINESS
            }
            return AegisException(
                kind = kind,
                code = code,
                message = message.ifEmpty { "AI 调用失败（HTTP $httpStatus）" },
                recovery = if (code in SIGNED_OUT_CODES) AegisRecovery.REAUTH else defaultRecovery(kind),
                httpStatus = httpStatus,
            )
        }

        /** 调用方取消。 */
        internal fun canceled(): AegisException = AegisException(
            Kind.NETWORK, 0, "调用已取消",
            cause = java.io.InterruptedIOException("canceled"),
            networkReason = AegisNetworkReason.CANCELED,
        )

        private fun defaultRecovery(kind: Kind): String = when (kind) {
            Kind.AUTH -> AegisRecovery.REFRESH_TOKEN
            else -> AegisRecovery.NONE
        }
    }
}
