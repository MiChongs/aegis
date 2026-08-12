package dev.aegis.sdk

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException

/**
 * Aegis **服务端**客户端。
 *
 * 与 [AegisClient] 的分工是「谁在调用」：
 *
 * | | [AegisClient] | [AegisServerClient] |
 * |---|---|---|
 * | 调用方 | 终端用户的 App / 网页 | 接入方**自己的后端** |
 * | 凭据 | 用户令牌（登录换来的） | 应用服务端密钥 `afk_…`（控制台签发） |
 * | 命名空间 | `/api/v1/apps/{appKey}/*` | `/api/apps/{appKey}/*` |
 * | 包装 | 按安全等级三档（签名 / 加密） | 纯 JSON —— 服务端之间没有"客户端被逆向"这个前提 |
 *
 * **密钥只能放在服务器上。** 把 `afk_…` 打进 APK 或前端包等于把它公开：
 * 谁拿到它都能问出任意用户的会员状态。它与 `appSecret` 是同一档要求。
 *
 * 被校验的用户由**访问令牌**指明 —— 客户端调你的接口时带上它，你原样转过来。
 * 不接受 userId：那等于让客户端自报身份（详见 [verifyMembership]）。
 *
 * ```kotlin
 * val server = AegisServerClient.builder("https://api.example.com", "demo_app", "afk_xxx").build()
 *
 * // 通用档：只问是不是会员
 * if (!server.isMember(userToken)) return deny()
 *
 * // 指定功能：问的是"他能不能用导出"
 * if (!server.hasFeature(userToken, "export")) return deny()
 *
 * // 要拿到期时间、是不是试用等字段时用完整版
 * val check = server.verifyMembership(userToken, feature = "export")
 * ```
 *
 * Java：
 * ```java
 * AegisServerClient server = AegisServerClient.builder(baseUrl, appKey, serverKey).build();
 * AegisMembershipCheck check = server.verifyMembership(userToken, "export");
 * ```
 *
 * 线程安全：可以（也应该）全进程共用一个实例。
 */
class AegisServerClient private constructor(
    private val baseUrl: HttpUrl,
    val appKey: String,
    private val serverKey: String,
    private val http: OkHttpClient,
) {

    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }

    /**
     * 校验**持有这个访问令牌的用户**当前是不是会员。
     *
     * [accessToken] 是用户登录后拿到的那个令牌，由客户端随请求带给你的服务端，
     * 你的服务端原样转过来即可。两样凭据各证明一件事，缺一不可：
     *
     *   服务端密钥 —— 证明「谁在问」（只有你的后端持有）
     *   访问令牌   —— 证明「问的是谁」（平台签发、平台验证）
     *
     * **刻意没有「按 userId 校验」的方法。** 那等于让调用方自报被查者的身份，
     * 而你的后端几乎一定会把这个身份交给它自己的客户端来说 —— 那条链路是
     * 「客户端说我是 42 → 你转发 42 → 我们回答 42 是会员 → 你放行发起请求的那个人」，
     * 攻击者只要知道任意一个会员的 userId 就能白嫖，服务端密钥拦不住这件事。
     * 需要按 userId 批量查（对账、到期提醒、客服）请走管理端接口。
     *
     * [feature] 留空即通用档（只问是不是会员）；给了就是问"他能不能用这个功能"。
     * 功能标识必须先在控制台的会员功能目录里登记 —— 传一个没登记的标识会得到
     * 40486 而不是静默的 false，因为拼错一个字母和"他没有这项权益"
     * 是完全不同的两件事。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun verifyMembership(accessToken: String, feature: String? = null): AegisMembershipCheck {
        require(accessToken.isNotBlank()) { "accessToken 不能为空" }
        val body = buildJsonObject {
            put("accessToken", JsonPrimitive(accessToken))
            if (!feature.isNullOrBlank()) put("feature", JsonPrimitive(feature))
        }
        val element = call("POST", "/vip/verify", body)
        return json.decodeFromJsonElement(AegisMembershipCheck.serializer(), element)
    }

    /**
     * 校验令牌持有者是否拥有某项功能权益，只回一个布尔。
     *
     * 适合写在自己的鉴权中间件里：`if (!server.hasFeature(token, "export")) return 403`。
     */
    @Throws(IOException::class)
    fun hasFeature(accessToken: String, feature: String): Boolean =
        verifyMembership(accessToken, feature).granted

    /** 校验令牌持有者是不是会员（不区分具体功能），只回一个布尔。 */
    @Throws(IOException::class)
    fun isMember(accessToken: String): Boolean = verifyMembership(accessToken).granted

    /**
     * 调用服务端命名空间下的任意接口（`/api/apps/{appKey}` 之下的相对路径）。
     *
     * 远程函数调用也在这个命名空间里：`call("POST", "/functions/xxx/invoke", body)`。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun call(method: String, path: String, body: JsonObject? = null): JsonElement {
        // addPathSegments 按 '/' 切段，**开头不能带斜杠**，否则会多出一个空段
        val url = baseUrl.newBuilder().addPathSegments("api/apps/$appKey/" + path.trimStart('/')).build()
        val payload = (body ?: JsonObject(emptyMap())).toString()
            .toRequestBody("application/json; charset=utf-8".toMediaType())
        val request = Request.Builder()
            .url(url)
            // 与远程函数调用同一把钥匙：再造一套"会员校验专用密钥"只会让
            // 接入方在服务器上配两份凭据，而它们的信任级别完全一样。
            .header("X-Aegis-Function-Key", serverKey)
            .method(method.uppercase(), payload)
            .build()

        val envelope = try {
            http.newCall(request).execute().use { response ->
                val text = response.body?.string().orEmpty()
                if (text.isBlank()) throw AegisException.network("HTTP ${response.code}：响应为空")
                json.decodeFromString(AegisEnvelope.serializer(), text)
            }
        } catch (error: AegisException) {
            throw error
        } catch (error: IOException) {
            throw AegisException.network("请求失败：${error.message}", error)
        }

        if (envelope.code == 200) return envelope.data ?: JsonNull
        // 服务端调用没有 /config 缓存（那是客户端的东西），因此不带错误目录：
        // 分类仍按业务码走，只是拿不到 hint。
        throw AegisException.fromCode(envelope.code, envelope.message)
    }

    class Builder internal constructor(
        baseUrl: String,
        private val appKey: String,
        private val serverKey: String,
    ) {
        private val normalizedBase = baseUrl.trimEnd('/').toHttpUrl()
        private var okHttp: OkHttpClient? = null

        /** 传入自己的 OkHttpClient（超时、代理、日志、连接池都在这里配）。 */
        fun httpClient(client: OkHttpClient) = apply { this.okHttp = client }

        fun build(): AegisServerClient {
            require(serverKey.startsWith("afk_")) {
                "服务端密钥应以 afk_ 开头，请在控制台「应用 → 远程函数 → 调用密钥」签发"
            }
            return AegisServerClient(normalizedBase, appKey, serverKey, okHttp ?: OkHttpClient())
        }
    }

    companion object {
        /**
         * @param baseUrl   服务端地址，如 `https://api.example.com`
         * @param appKey    应用标识
         * @param serverKey 应用服务端密钥（`afk_…`），控制台签发，**只放在服务器上**
         */
        @JvmStatic
        fun builder(baseUrl: String, appKey: String, serverKey: String): Builder =
            Builder(baseUrl, appKey, serverKey)
    }
}

/**
 * 服务端会员校验的结论。
 *
 * 放行只看 [granted] 一个字段：没指定功能标识时它等于"是不是会员"，
 * 指定了则还要求当前权益覆盖那个标识。其余字段是给日志、给提示文案、
 * 给「还剩几天，要不要提醒续费」这类判断用的。
 */
@Serializable
data class AegisMembershipCheck(
    val granted: Boolean = false,
    /** 是否定位到了这个用户。定位不到会直接抛异常，因此这里恒为 true。 */
    val matched: Boolean = false,
    val userId: Long = 0,
    val account: String = "",
    val membership: AegisMembership = AegisMembership(),
    /** 指定了功能标识时才有 */
    val feature: AegisFeatureVerdict? = null,
    /** 服务端做出这个判定的时刻（RFC3339）。要缓存结论时按它算 TTL，别用本地时间。 */
    val checkedAt: String = "",
)

/** 会员字段。 */
@Serializable
data class AegisMembership(
    @SerialName("isVip") val isVip: Boolean = false,
    /** 当前这段会员期是不是试用给的 —— 决定该引导"升级"还是"续费" */
    @SerialName("isTrial") val isTrial: Boolean = false,
    /** none / trial / wallet / payment_order / admin_grant / unknown */
    val source: String = "none",
    val planName: String = "",
    val expireAt: String? = null,
    val remainingSeconds: Long = 0,
    val remainingDays: Int = 0,
    /** 当前生效的功能标识；不是会员时为空 */
    val features: List<String> = emptyList(),
) {
    /** 当前权益是否覆盖某个功能标识（本地判断，不再发请求）。 */
    fun hasFeature(tag: String): Boolean = isVip && features.contains(tag.trim().lowercase())
}

/** 针对某个功能标识的判定。 */
@Serializable
data class AegisFeatureVerdict(
    val tag: String = "",
    val name: String = "",
    val granted: Boolean = false,
)
