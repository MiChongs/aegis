package dev.aegis.sdk

import java.io.IOException

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
) : IOException(message, cause) {

    enum class Kind { TRANSPORT, AUTH, BUSINESS, NETWORK }

    /** 拿 refreshToken 换一次新令牌就能继续。 */
    val isRetryableWithRefresh: Boolean get() = recovery == AegisRecovery.REFRESH_TOKEN

    /** 重新拉 /config 后重试一次即可（典型场景：传输密钥刚轮换过）。 */
    val isRetryableAfterConfigRefresh: Boolean get() = recovery == AegisRecovery.REFRESH_CONFIG

    override fun toString(): String =
        "AegisException(kind=$kind, code=$code, message=$message" +
            (if (hint.isNotEmpty()) ", hint=$hint" else "") + ")"

    companion object {
        /** 网关级错误码区间：这些错在请求进到业务逻辑之前就被拦下了。 */
        private val TRANSPORT_CODES = setOf(
            40070, 40071, 40072, 40073, 40074, 40075, 40076, 40077, 40078, 40079,
            40084, 40174, 40175, 40176, 40970, 42670, 50370, 50372,
        )
        private val AUTH_CODES = setOf(40100, 40110, 40372)

        @JvmStatic
        fun transport(message: String, cause: Throwable? = null): AegisException =
            AegisException(Kind.TRANSPORT, 0, message, cause = cause)

        @JvmStatic
        fun network(message: String, cause: Throwable? = null): AegisException =
            AegisException(Kind.NETWORK, 0, message, cause = cause)

        /**
         * 按业务码归类。[descriptor] 来自 /config 的 errors 目录 ——
         * 有它就能给出「该怎么办」，没有也不影响归类。
         */
        @JvmStatic
        fun fromCode(code: Int, message: String, descriptor: ErrorDescriptor? = null): AegisException {
            val kind = when (code) {
                in AUTH_CODES -> Kind.AUTH
                in TRANSPORT_CODES -> Kind.TRANSPORT
                else -> Kind.BUSINESS
            }
            return AegisException(
                kind = kind,
                code = code,
                message = message.ifEmpty { descriptor?.message ?: "请求失败" },
                recovery = descriptor?.recovery ?: defaultRecovery(kind),
                hint = descriptor?.hint.orEmpty(),
            )
        }

        private fun defaultRecovery(kind: Kind): String = when (kind) {
            Kind.AUTH -> AegisRecovery.REFRESH_TOKEN
            else -> AegisRecovery.NONE
        }
    }
}
